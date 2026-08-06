package payment

import (
	"fmt"
	"os"
	"strings"
)

// AutoCloseReplaceEnv 环境变量名。生产 compose 必须显式 false。
const AutoCloseReplaceEnv = "PAY_AUTO_CLOSE_REPLACE"

// ErrFeatureNotImplemented 生产拒绝启用未完成的自动关单/替换。
var ErrFeatureNotImplemented = fmt.Errorf("payment: FEATURE_NOT_IMPLEMENTED: auto close/replace is disabled and not available")

// ValidateAutoCloseReplaceConfig 进程启动时调用。
// PAY_AUTO_CLOSE_REPLACE=true/1 时 fail-closed 拒绝启动，绝不调用 CloseOrder。
func ValidateAutoCloseReplaceConfig() error {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(AutoCloseReplaceEnv)))
	switch v {
	case "", "0", "false", "off", "no":
		return nil
	default:
		return fmt.Errorf("%w: set %s=false (got %q); auto close/replacement deferred", ErrFeatureNotImplemented, AutoCloseReplaceEnv, os.Getenv(AutoCloseReplaceEnv))
	}
}

// AutoCloseReplaceEnabled 恒为 false：半截 recovery 已移除，任何配置不得开启。
func AutoCloseReplaceEnabled() bool {
	return false
}
