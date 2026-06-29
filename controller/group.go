package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/grouphook"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groupNames,
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]interface{})
	userGroup := ""
	userId := c.GetInt("id")
	userGroup, _ = model.GetUserGroup(userId, false)
	userUsableGroups := service.GetUserUsableGroups(userGroup)
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			// mt: 2D —— 建 Key 下拉**只显示模型分组**（排除层级/default），倍率=2D 有效扣费值（含代理租户覆盖）。
			// 用户建 Key 只选模型分组；层级由管理员/代理分配、不可自选。未装配钩子时维持原生行为。
			if grouphook.ModelGroupDropdownResolver != nil {
				eff, isModelGroup := grouphook.ModelGroupDropdownResolver(int64(userId), userGroup, groupName)
				if !isModelGroup {
					continue // 非模型分组（层级/default）不进下拉
				}
				usableGroups[groupName] = map[string]interface{}{"ratio": eff, "desc": desc}
				continue
			}
			usableGroups[groupName] = map[string]interface{}{
				"ratio": service.GetUserGroupRatio(userGroup, groupName),
				"desc":  desc,
			}
		}
	}
	// "auto" 特殊组：2D 装配时也排除（建 Key 只留模型分组）。
	if grouphook.ModelGroupDropdownResolver == nil {
		if _, ok := userUsableGroups["auto"]; ok {
			usableGroups["auto"] = map[string]interface{}{
				"ratio": "自动",
				"desc":  setting.GetUsableGroupDescription("auto"),
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
