package common

import (
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	"github.com/shirou/gopsutil/cpu"
)

// Monitor 定时监控cpu使用率，超过阈值输出pprof文件
func Monitor() {
	for {
		percent, err := cpu.Percent(time.Second, false)
		if err != nil {
			SysLog("读取 CPU 使用率失败 " + err.Error())
			time.Sleep(30 * time.Second)
			continue
		}
		if len(percent) > 0 && percent[0] > 80 {
			fmt.Println("cpu usage too high")
			// write pprof file
			if _, err := os.Stat("./pprof"); os.IsNotExist(err) {
				err := os.Mkdir("./pprof", 0700)
				if err != nil {
					SysLog("创建pprof文件夹失败 " + err.Error())
					continue
				}
			}
			if err := os.Chmod("./pprof", 0700); err != nil {
				SysLog("收紧pprof目录权限失败 " + err.Error())
				continue
			}
			profilePath := "./pprof/" + fmt.Sprintf("cpu-%s.pprof", time.Now().Format("20060102150405"))
			f, err := os.OpenFile(profilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				SysLog("创建pprof文件失败 " + err.Error())
				continue
			}
			err = pprof.StartCPUProfile(f)
			if err != nil {
				SysLog("启动pprof失败 " + err.Error())
				_ = f.Close()
				_ = os.Remove(profilePath)
				continue
			}
			time.Sleep(10 * time.Second)
			pprof.StopCPUProfile()
			if err := f.Close(); err != nil {
				SysLog("关闭pprof文件失败 " + err.Error())
			}
		}
		time.Sleep(30 * time.Second)
	}
}
