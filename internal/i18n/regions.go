package i18n

// The strings of the common regions, kept apart from the main tables so
// that work on other parts merges cleanly.

func init() {
	for k, v := range jaRegions {
		jaTable[k] = v
	}
	for k, v := range hantRegions {
		hantTable[k] = v
	}
}

var jaRegions = map[string]string{
	"更多 %d":  "他 %d 件",
	"调整常用":   "よく使う地域を編集",
	"点选常用地区": "よく使う地域を選択",
	"%s 前移":  "%s を前へ移動",
}

var hantRegions = map[string]string{
	"更多 %d":  "更多 %d",
	"调整常用":   "調整常用",
	"点选常用地区": "點選常用地區",
	"%s 前移":  "%s 前移",
}
