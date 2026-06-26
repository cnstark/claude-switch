package config

import (
	"fmt"
	"strings"

	"github.com/cnstark/claude-switch/internal/logging"
)

// Validate 校验 Config 的所有规则，返回第一个错误或 nil
func Validate(cfg Config) error {
	// 1. 至少配置一个 private key
	if len(cfg.Server.PrivateKeys) == 0 {
		return fmt.Errorf("server.private_keys: 至少需要配置一个私有 key")
	}

	// 2. upstream name 唯一且非空
	seenUpstream := make(map[string]bool)
	for _, u := range cfg.Upstreams {
		if u.Name == "" {
			return fmt.Errorf("upstreams: cfg 名不能为空")
		}
		if seenUpstream[u.Name] {
			return fmt.Errorf("upstreams: cfg 名 %q 重复", u.Name)
		}
		seenUpstream[u.Name] = true
	}

	// 3. project name 唯一且非空
	seenProject := make(map[string]bool)
	for _, p := range cfg.Projects {
		if p.Name == "" {
			return fmt.Errorf("projects: 项目名不能为空")
		}
		if seenProject[p.Name] {
			return fmt.Errorf("projects: 项目名 %q 重复", p.Name)
		}
		seenProject[p.Name] = true
	}

	// 4. private key 指向存在的 project
	seenKeys := make(map[string]bool)
	for key, projName := range cfg.Server.PrivateKeys {
		if seenKeys[key] {
			return fmt.Errorf("server.private_keys: key %q 重复", logging.MaskKey(key))
		}
		seenKeys[key] = true
		if !seenProject[projName] {
			return fmt.Errorf("server.private_keys: key %q 指向不存在的项目 %q", logging.MaskKey(key), projName)
		}
	}

	// 5. model_map 引用的 cfg 名必须存在
	for _, p := range cfg.Projects {
		for reqModel, cfgList := range p.ModelMap {
			if len(cfgList) == 0 {
				return fmt.Errorf("projects.%s.model_map.%s: cfg 列表不能为空", p.Name, reqModel)
			}
			for _, cfgName := range cfgList {
				if !seenUpstream[cfgName] {
					return fmt.Errorf("projects.%s.model_map.%s: 引用了不存在的 cfg %q", p.Name, reqModel, cfgName)
				}
			}
		}
	}

	// 5.1 model_map 别名不得与 upstream.name 冲突（保证 allow_direct_access 路由无歧义）
	for _, p := range cfg.Projects {
		for reqModel := range p.ModelMap {
			if seenUpstream[reqModel] {
				return fmt.Errorf(
					"projects.%s.model_map: 别名 %q 与 upstream 名冲突"+
						"（allow_direct_access 要求别名与 cfg 名空间不重叠）",
					p.Name, reqModel,
				)
			}
		}
	}

	// 6. retry_backoff 校验
	for _, u := range cfg.Upstreams {
		if len(u.RetryBackoff) > 4 {
			return fmt.Errorf("upstreams.%s.retry_backoff: 最多支持 4 档退避时间，当前 %d 档", u.Name, len(u.RetryBackoff))
		}
		for i, d := range u.RetryBackoff {
			if d <= 0 {
				return fmt.Errorf("upstreams.%s.retry_backoff[%d]: 退避时间必须为正数，当前 %s", u.Name, i, d)
			}
		}
	}

	// 7. project.log_level 合法性（meta 兼容旧配置，info 为新值）
	for _, p := range cfg.Projects {
		switch p.LogLevel {
		case "", LogOff, LogMeta, LogInfo, LogDebug:
			// 合法
		default:
			return fmt.Errorf("projects.%s.log_level: 无效值 %q（允许: %s）", p.Name, p.LogLevel, strings.Join(validLogLevelsStr(true), ", "))
		}
	}

	// 8. server.log_level 合法性（server 不支持 meta）
	switch cfg.Server.LogLevel {
	case "", LogOff, LogInfo, LogDebug:
		// 合法
	default:
		return fmt.Errorf("server.log_level: 无效值 %q（允许: %s）", cfg.Server.LogLevel, strings.Join(validLogLevelsStr(false), ", "))
	}

	// 9. server.log_max_days 范围（nil 已由 NewSnapshot 填充默认值，此处仅校验 >=0）
	if cfg.Server.LogMaxDays != nil && *cfg.Server.LogMaxDays < 0 {
		return fmt.Errorf("server.log_max_days: 不能为负数，当前 %d", *cfg.Server.LogMaxDays)
	}

	return nil
}

// validLogLevelsStr 返回允许的日志级别字符串，用于校验错误信息。
// includeMeta=true 时包含 meta（project 兼容旧配置），false 时排除（server 不支持 meta）。
func validLogLevelsStr(includeMeta bool) []string {
	levels := []string{string(LogOff)}
	if includeMeta {
		levels = append(levels, string(LogMeta))
	}
	levels = append(levels, string(LogInfo), string(LogDebug))
	return levels
}
