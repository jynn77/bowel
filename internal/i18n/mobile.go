package i18n

// The strings only the phone app's bridge (mobile/bridge) writes, kept apart
// from the main tables so that work on other parts merges cleanly.

func init() {
	for k, v := range jaMobile {
		jaTable[k] = v
	}
	for k, v := range hantMobile {
		hantTable[k] = v
	}
}

var jaMobile = map[string]string{
	"文件太大，VPN 的内存放不下，请换精简版的地址": "ファイルが大きすぎて VPN のメモリに収まりません。軽量版の URL を使ってください",
	"下载到的不是 Geo 数据": "ダウンロードしたものは Geo データではありません",
	"这台设备不能更新 Geo 数据": "この端末では Geo データを更新できません",
	"「%s」的范围里没有可用节点":  "「%s」の範囲に使えるノードがありません",
	"未命中，走默认":         "ルールに一致せず、デフォルトへ",
	"mihomo · 运行中":    "mihomo · 実行中",
	"域名 ":             "ドメイン ",
	"这条路由现在没有节点":      "このルートには今ノードがありません",
	"mihomo · 未运行":    "mihomo · 停止中",
	"IP 段 ":           "IP 範囲 ",
	"未接管时不能这样做":       "オンのときだけ実行できます",
	"没有命中域名规则：国内网站和局域网按 GeoSite / GeoIP 走「直连」，其余走「默认」。": "ドメインルールに一致しません。国内サイトと LAN は GeoSite / GeoIP で「直接接続」、それ以外は「デフォルト」へ流れます。",
	"图片里没有二维码":     "画像に QR コードがありません",
	"还没有 Provider": "まだ Provider がありません",
	"域名后缀 ":        "ドメインサフィックス ",
	"「%s」固定的节点不通":  "「%s」の固定ノードに接続できません",
	"域名关键词 ":       "ドメインキーワード ",
	"上海":           "上海",
}

var hantMobile = map[string]string{
	"文件太大，VPN 的内存放不下，请换精简版的地址": "檔案太大，VPN 的記憶體放不下，請換精簡版的網址",
	"下载到的不是 Geo 数据": "下載到的不是 Geo 資料",
	"这台设备不能更新 Geo 数据": "這台裝置無法更新 Geo 資料",
	"「%s」的范围里没有可用节点":  "「%s」的範圍內沒有可用節點",
	"未命中，走默认":         "未命中，走預設",
	"mihomo · 运行中":    "mihomo · 執行中",
	"域名 ":             "網域 ",
	"这条路由现在没有节点":      "這條路由現在沒有節點",
	"mihomo · 未运行":    "mihomo · 未執行",
	"IP 段 ":           "IP 區段 ",
	"未接管时不能这样做":       "未連線時無法這樣做",
	"没有命中域名规则：国内网站和局域网按 GeoSite / GeoIP 走「直连」，其余走「默认」。": "沒有命中網域規則：國內網站和區域網路按 GeoSite / GeoIP 走「直連」，其餘走「預設」。",
	"图片里没有二维码":     "圖片裡沒有 QR Code",
	"还没有 Provider": "還沒有 Provider",
	"域名后缀 ":        "網域後綴 ",
	"「%s」固定的节点不通":  "「%s」固定的節點不通",
	"域名关键词 ":       "網域關鍵字 ",
	"上海":           "上海",
}
