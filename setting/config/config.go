package config

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// ConfigManager 统一管理所有配置
type ConfigManager struct {
	configs map[string]interface{}
	mutex   sync.RWMutex
}

var GlobalConfig = NewConfigManager()

func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		configs: make(map[string]interface{}),
	}
}

// Register 注册一个配置模块
func (cm *ConfigManager) Register(name string, config interface{}) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	cm.configs[name] = config
}

// Get 获取指定配置模块
func (cm *ConfigManager) Get(name string) interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return cm.configs[name]
}

// ValidateUpdate validates a registered config update against an isolated
// candidate without publishing it. The boolean reports whether name exists.
func (cm *ConfigManager) ValidateUpdate(name string, configMap map[string]string) (bool, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	config, ok := cm.configs[name]
	if !ok {
		return false, nil
	}
	return true, validateConfigFromMap(config, configMap)
}

// ApplyUpdate validates and publishes one registered config update while
// serializing it with config registration/export/load operations.
func (cm *ConfigManager) ApplyUpdate(name string, configMap map[string]string) (bool, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()
	config, ok := cm.configs[name]
	if !ok {
		return false, nil
	}
	return true, updateConfigFromMap(config, configMap)
}

// LoadFromDB 从数据库加载配置
func (cm *ConfigManager) LoadFromDB(options map[string]string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for name, config := range cm.configs {
		prefix := name + "."
		configMap := make(map[string]string)

		// 收集属于此配置的所有选项
		for key, value := range options {
			if strings.HasPrefix(key, prefix) {
				configKey := strings.TrimPrefix(key, prefix)
				configMap[configKey] = value
			}
		}

		// 如果找到配置项，则更新配置
		if len(configMap) > 0 {
			if err := updateConfigFromMap(config, configMap); err != nil {
				common.SysError("failed to update config " + name + ": " + err.Error())
				continue
			}
		}
	}

	return nil
}

// SaveToDB 将配置保存到数据库
func (cm *ConfigManager) SaveToDB(updateFunc func(key, value string) error) error {
	cm.mutex.RLock()
	values := make(map[string]string)
	for name, config := range cm.configs {
		configMap, err := configToMap(config)
		if err != nil {
			cm.mutex.RUnlock()
			return err
		}
		for key, value := range configMap {
			values[name+"."+key] = value
		}
	}
	cm.mutex.RUnlock()

	for key, value := range values {
		if err := updateFunc(key, value); err != nil {
			return err
		}
	}

	return nil
}

// 辅助函数：将配置对象转换为map
func configToMap(config interface{}) (map[string]string, error) {
	result := make(map[string]string)

	val := reflect.ValueOf(config)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		// 获取json标签作为键名
		key := fieldType.Tag.Get("json")
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		// 处理不同类型的字段
		var strValue string
		switch field.Kind() {
		case reflect.String:
			strValue = field.String()
		case reflect.Bool:
			strValue = strconv.FormatBool(field.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			strValue = strconv.FormatInt(field.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			strValue = strconv.FormatUint(field.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			strValue = strconv.FormatFloat(field.Float(), 'f', -1, 64)
		case reflect.Ptr:
			// 处理指针类型：如果非 nil，序列化指向的值
			if !field.IsNil() {
				bytes, err := common.Marshal(field.Interface())
				if err != nil {
					return nil, err
				}
				strValue = string(bytes)
			} else {
				// nil 指针序列化为 "null"
				strValue = "null"
			}
		case reflect.Map, reflect.Slice, reflect.Struct:
			// 复杂类型使用JSON序列化
			bytes, err := common.Marshal(field.Interface())
			if err != nil {
				return nil, err
			}
			strValue = string(bytes)
		default:
			// 跳过不支持的类型
			continue
		}

		result[key] = strValue
	}

	return result, nil
}

// 辅助函数：从map更新配置对象
func updateConfigFromMap(config interface{}, configMap map[string]string) error {
	target := reflect.ValueOf(config)
	if target.Kind() != reflect.Ptr || target.IsNil() || target.Elem().Kind() != reflect.Struct {
		return errors.New("config must be a non-nil pointer to a struct")
	}
	candidate := reflect.New(target.Elem().Type()).Elem()
	candidate.Set(target.Elem())
	if err := applyConfigMap(candidate, configMap); err != nil {
		return err
	}
	target.Elem().Set(candidate)
	return nil
}

func validateConfigFromMap(config interface{}, configMap map[string]string) error {
	target := reflect.ValueOf(config)
	if target.Kind() != reflect.Ptr || target.IsNil() || target.Elem().Kind() != reflect.Struct {
		return errors.New("config must be a non-nil pointer to a struct")
	}
	candidate := reflect.New(target.Elem().Type()).Elem()
	candidate.Set(target.Elem())
	return applyConfigMap(candidate, configMap)
}

func applyConfigMap(candidate reflect.Value, configMap map[string]string) error {
	typ := candidate.Type()
	matched := make(map[string]bool, len(configMap))
	for i := 0; i < candidate.NumField(); i++ {
		field := candidate.Field(i)
		fieldType := typ.Field(i)

		// 跳过未导出字段
		if !fieldType.IsExported() {
			continue
		}

		// 获取json标签作为键名
		key := fieldType.Tag.Get("json")
		if key == "" || key == "-" {
			key = fieldType.Name
		}

		// 检查map中是否有对应的值
		strValue, ok := configMap[key]
		if !ok {
			continue
		}
		matched[key] = true

		// 根据字段类型设置值
		if !field.CanSet() {
			continue
		}

		switch field.Kind() {
		case reflect.String:
			field.SetString(strValue)
		case reflect.Bool:
			boolValue, err := strconv.ParseBool(strValue)
			if err != nil {
				return fmt.Errorf("invalid boolean config %s: %w", key, err)
			}
			field.SetBool(boolValue)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			intValue, err := strconv.ParseInt(strValue, 10, 64)
			if err != nil {
				// 兼容 float 格式的字符串（如 "2.000000"）
				floatValue, fErr := strconv.ParseFloat(strValue, 64)
				if fErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
					return fmt.Errorf("invalid integer config %s: %w", key, err)
				}
				intValue = int64(floatValue)
			}
			if field.OverflowInt(intValue) {
				return fmt.Errorf("integer config %s overflows %s", key, field.Type())
			}
			field.SetInt(intValue)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			uintValue, err := strconv.ParseUint(strValue, 10, 64)
			if err != nil {
				// 兼容 float 格式的字符串
				floatValue, fErr := strconv.ParseFloat(strValue, 64)
				if fErr != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) || floatValue < 0 {
					return fmt.Errorf("invalid unsigned integer config %s: %w", key, err)
				}
				uintValue = uint64(floatValue)
			}
			if field.OverflowUint(uintValue) {
				return fmt.Errorf("unsigned integer config %s overflows %s", key, field.Type())
			}
			field.SetUint(uintValue)
		case reflect.Float32, reflect.Float64:
			floatValue, err := strconv.ParseFloat(strValue, field.Type().Bits())
			if err != nil || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
				return fmt.Errorf("invalid floating-point config %s", key)
			}
			field.SetFloat(floatValue)
		case reflect.Ptr:
			// 处理指针类型
			if strValue == "null" {
				field.Set(reflect.Zero(field.Type()))
			} else {
				fresh := reflect.New(field.Type().Elem())
				if !field.IsNil() {
					fresh.Elem().Set(field.Elem())
				}
				if err := common.Unmarshal([]byte(strValue), fresh.Interface()); err != nil {
					return fmt.Errorf("invalid pointer config %s: %w", key, err)
				}
				field.Set(fresh)
			}
		case reflect.Map:
			// JSON unmarshalling merges into existing maps (keeps old keys that are
			// absent from the new JSON). Allocate a fresh map so removed keys
			// are properly cleared.
			fresh := reflect.New(field.Type())
			if err := common.Unmarshal([]byte(strValue), fresh.Interface()); err != nil {
				return fmt.Errorf("invalid map config %s: %w", key, err)
			}
			field.Set(fresh.Elem())
		case reflect.Slice, reflect.Struct:
			fresh := reflect.New(field.Type())
			if err := common.Unmarshal([]byte(strValue), fresh.Interface()); err != nil {
				return fmt.Errorf("invalid structured config %s: %w", key, err)
			}
			field.Set(fresh.Elem())
		}
	}
	for key := range configMap {
		if !matched[key] {
			return fmt.Errorf("unknown config key %s", key)
		}
	}
	return nil
}

// ConfigToMap 将配置对象转换为map（导出函数）
func ConfigToMap(config interface{}) (map[string]string, error) {
	return configToMap(config)
}

// UpdateConfigFromMap 从map更新配置对象（导出函数）
func UpdateConfigFromMap(config interface{}, configMap map[string]string) error {
	return updateConfigFromMap(config, configMap)
}

// ExportAllConfigs 导出所有已注册的配置为扁平结构
func (cm *ConfigManager) ExportAllConfigs() map[string]string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	result := make(map[string]string)

	for name, cfg := range cm.configs {
		configMap, err := ConfigToMap(cfg)
		if err != nil {
			continue
		}

		// 使用 "模块名.配置项" 的格式添加到结果中
		for key, value := range configMap {
			result[name+"."+key] = value
		}
	}

	return result
}
