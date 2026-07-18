package admin

import _ "embed"

// DashboardHTML 嵌入的仪表盘 HTML 文件
var DashboardHTML []byte

//go:embed dashboard.html
var dashboardRaw string

func init() {
	DashboardHTML = []byte(dashboardRaw)
}
