package service

import (
	"apigo.cc/go/cast"
	"apigo.cc/go/log"
	"reflect"
	"regexp"
	"strings"
	"sync"
)

// VerifyType 校验类型
type VerifyType uint8

const (
	VerifyUnknown VerifyType = iota
	VerifyRegex
	VerifyStringLength
	VerifyGreaterThan
	VerifyLessThan
	VerifyBetween
	VerifyInList
	VerifyByFunc
)

// VerifySet 校验规则集
type VerifySet struct {
	Type       VerifyType
	Regex      *regexp.Regexp
	StringArgs []string
	IntArgs    []int
	FloatArgs  []float64
	Func       func(any, []string) bool
}

var (
	verifySets          = make(map[string]*VerifySet)
	verifySetsLock      = sync.RWMutex{}
	verifyFunctions     = make(map[string]func(any, []string) bool)
	verifyFunctionsLock = sync.RWMutex{}
)

// RegisterVerifyFunc 注册自定义校验函数
func RegisterVerifyFunc(name string, f func(in any, args []string) bool) {
	verifyFunctionsLock.Lock()
	verifyFunctions[name] = f
	verifyFunctionsLock.Unlock()
}

// RegisterVerify 注册预定义校验规则
func RegisterVerify(name, setting string) {
	set, _ := compileVerifySet(setting)
	if set != nil {
		verifySetsLock.Lock()
		verifySets[name] = set
		verifySetsLock.Unlock()
	}
}

// VerifyStruct 校验结构体
func VerifyStruct(in any, logger *log.Logger) (ok bool, field string) {
	v := cast.RealValue(reflect.ValueOf(in))
	if v.Kind() != reflect.Struct {
		if logger != nil {
			logger.Error("verify input is not struct", "type", v.Type().String())
		}
		return false, ""
	}

	for i := 0; i < v.NumField(); i++ {
		ft := v.Type().Field(i)
		fv := v.Field(i)

		// 忽略空指针、空切片、空 Map
		if (fv.Kind() == reflect.Ptr && fv.IsNil()) ||
			(fv.Kind() == reflect.Slice && fv.Len() == 0) ||
			(fv.Kind() == reflect.Map && fv.Len() == 0) {
			continue
		}

		if ft.Anonymous {
			// 处理嵌套结构体（继承）
			if fv.CanInterface() {
				if ok, f := VerifyStruct(fv.Interface(), logger); !ok {
					return false, f
				}
			}
			continue
		}

		tag := ft.Tag.Get("verify")
		keyTag := ft.Tag.Get("verifyKey")
		if tag != "" || keyTag != "" {
			var err error
			ok, f, err := _verifyValue(fv, tag, keyTag, logger)
			if !ok {
				if f == "" {
					f = cast.GetLowerName(ft.Name)
				}
				if logger != nil {
					if err != nil {
						logger.Error(err.Error(), "field", f)
					} else {
						logger.Warning("verify failed", "field", f, "tag", tag)
					}
				}
				return false, f
			}
		}
	}
	return true, ""
}

func _verifyValue(in reflect.Value, setting, keySetting string, logger *log.Logger) (bool, string, error) {
	t := in.Type()
	// 处理切片 (非 byte 切片)
	if t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Uint8 {
		if setting != "" {
			for i := 0; i < in.Len(); i++ {
				if ok, f, err := _verifyValue(in.Index(i), setting, "", logger); !ok {
					return false, f, err
				}
			}
		}
		return true, "", nil
	}

	// 处理 Map
	if t.Kind() == reflect.Map {
		for _, k := range in.MapKeys() {
			if keySetting != "" {
				if ok, _, err := _verifyValue(k, keySetting, "", logger); !ok {
					return false, "key", err
				}
			}
			if setting != "" {
				if ok, f, err := _verifyValue(in.MapIndex(k), setting, "", logger); !ok {
					return false, f, err
				}
			}
		}
		return true, "", nil
	}

	// 处理嵌套 Struct
	if t.Kind() == reflect.Struct {
		ok, f := VerifyStruct(in.Interface(), logger)
		return ok, f, nil
	}

	// 基础校验
	if setting == "" {
		return true, "", nil
	}

	ok, err := verify(in.Interface(), setting)
	return ok, "", err
}

func verify(in any, setting string) (bool, error) {
	if len(setting) < 2 {
		return false, nil
	}

	verifySetsLock.RLock()
	set, exists := verifySets[setting]
	verifySetsLock.RUnlock()

	if !exists {
		var err error
		set, err = compileVerifySet(setting)
		if err != nil {
			return false, err
		}
		verifySetsLock.Lock()
		verifySets[setting] = set
		verifySetsLock.Unlock()
	}

	switch set.Type {
	case VerifyByFunc:
		return set.Func(in, set.StringArgs), nil
	case VerifyRegex:
		return set.Regex.MatchString(cast.String(in)), nil
	case VerifyStringLength:
		l := len(cast.String(in))
		if len(set.StringArgs) > 0 {
			if set.StringArgs[0] == "+" {
				return l >= set.IntArgs[0], nil
			} else if set.StringArgs[0] == "-" {
				return l <= set.IntArgs[0], nil
			}
		}
		if len(set.IntArgs) > 1 {
			return l >= set.IntArgs[0] && l <= set.IntArgs[1], nil
		}
		return l == set.IntArgs[0], nil
	case VerifyGreaterThan:
		return cast.Float64(in) > set.FloatArgs[0], nil
	case VerifyLessThan:
		return cast.Float64(in) < set.FloatArgs[0], nil
	case VerifyBetween:
		val := cast.Float64(in)
		return val >= set.FloatArgs[0] && val <= set.FloatArgs[1], nil
	case VerifyInList:
		s := cast.String(in)
		for _, item := range set.StringArgs {
			if item == s {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

func compileVerifySet(setting string) (*VerifySet, error) {
	set := &VerifySet{Type: VerifyUnknown}
	if setting == "" {
		return set, nil
	}

	if setting[0] != '^' {
		key := setting
		args := ""
		if pos := strings.IndexByte(setting, ':'); pos != -1 {
			key = setting[:pos]
			args = setting[pos+1:]
		}

		// 优先查找自定义函数
		verifyFunctionsLock.RLock()
		f, exists := verifyFunctions[key]
		verifyFunctionsLock.RUnlock()
		if exists {
			set.Type = VerifyByFunc
			set.Func = f
			if args != "" {
				set.StringArgs = strings.Split(args, ",")
			}
			return set, nil
		}

		// 内置规则
		switch key {
		case "length":
			set.Type = VerifyStringLength
			if args == "" {
				args = "1+"
			}
			last := args[len(args)-1]
			if last == '+' || last == '-' {
				set.StringArgs = []string{string(last)}
				args = args[:len(args)-1]
			}
			// 同时支持逗号和中划线
			sep := ","
			if strings.Contains(args, "-") && !strings.Contains(args, ",") {
				sep = "-"
			}
			if strings.Contains(args, sep) {
				a := strings.Split(args, sep)
				set.IntArgs = []int{cast.Int(a[0]), cast.Int(a[1])}
			} else {
				set.IntArgs = []int{cast.Int(args)}
			}
			return set, nil
		case "between":
			set.Type = VerifyBetween
			if args == "" {
				args = "1-100000000"
			}
			a := strings.Split(args, "-")
			if len(a) == 1 {
				set.FloatArgs = []float64{0, cast.Float64(a[0])}
			} else {
				set.FloatArgs = []float64{cast.Float64(a[0]), cast.Float64(a[1])}
			}
			return set, nil
		case "gt":
			set.Type = VerifyGreaterThan
			set.FloatArgs = []float64{cast.Float64(args)}
			return set, nil
		case "lt":
			set.Type = VerifyLessThan
			set.FloatArgs = []float64{cast.Float64(args)}
			return set, nil
		case "in":
			set.Type = VerifyInList
			if args != "" {
				set.StringArgs = strings.Split(args, ",")
			}
			return set, nil
		}
	}

	// 默认视为正则表达式
	rx, err := regexp.Compile(setting)
	if err != nil {
		return nil, err
	}
	set.Type = VerifyRegex
	set.Regex = rx
	return set, nil
}
