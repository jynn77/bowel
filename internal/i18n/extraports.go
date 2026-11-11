package i18n

// The strings of the extra ports (core.ExtraPort), kept apart from the main
// tables so that work on other parts merges cleanly.

func init() {
	for k, v := range jaExtraPorts {
		jaTable[k] = v
	}
	for k, v := range hantExtraPorts {
		hantTable[k] = v
	}
}

var jaExtraPorts = map[string]string{
	"端口 %d 没有写路由组":         "ポート %d にルートグループが指定されていません",
	"端口 %d 指向不存在的路由组: %s": "ポート %d は存在しないルートグループを指しています: %s",
}

var hantExtraPorts = map[string]string{
	"端口 %d 没有写路由组":         "連接埠 %d 沒有寫路由組",
	"端口 %d 指向不存在的路由组: %s": "連接埠 %d 指向不存在的路由組: %s",
}
