package main

// Skins: the interface's looks as a whole. A skin is a palette for the
// light Mac and one for the dark, or one only, which it keeps whatever the
// Mac is set to; its faces, for text and for the big type; how round its
// corners are; a glow for the big type and what is pressed; and a texture
// laid over the window, under nothing that takes input. What the
// interface does stays the same in every skin: only how it is drawn
// changes. The theme color stays the user's, but picking a skin picks its
// own with it (skinPick).

import (
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/oboo/moho/internal/i18n"
	"github.com/oboo/moho/internal/settings"
)

type skin struct {
	id     string
	zh, en string
	// note says what it is like, in each language.
	noteZh, noteEn string
	// light and dark are its palettes; one nil, it keeps the other.
	light, dark *pal
	// accent is the theme color picking it sets.
	accent string
}

func (k *skin) name() string { return i18n.T(k.zh, k.en) }
func (k *skin) note() string { return i18n.T(k.noteZh, k.noteEn) }

// pal is its palette for a Mac in the dark or not.
func (k *skin) pal(dark bool) *pal {
	if k.light == nil || dark && k.dark != nil {
		return k.dark
	}
	return k.light
}

// fixed is the appearance it keeps, "" when it follows the setting.
func (k *skin) fixed() string {
	switch {
	case k.light == nil:
		return "dark"
	case k.dark == nil:
		return "light"
	}
	return ""
}

const (
	faceMono    = "SF Mono, Menlo, PingFang SC, monospace"
	faceSerif   = "Georgia, Songti SC, serif"
	faceDIN     = "DIN Alternate, OPPO Sans 4.0, PingFang SC"
	faceFutura  = "Futura, OPPO Sans 4.0, PingFang SC"
	faceRounded = "Arial Rounded MT Bold, OPPO Sans 4.0, PingFang SC"
)

var (
	broadsheetLight = pal{
		paper: ui.Hex("#efe7d4"), paper2: ui.Hex("#e7dec8"), card: ui.Hex("#f5eedd"), sheet: ui.Hex("#f8f2e4"),
		ink: ui.Hex("#1b1712"), ink2: ui.Hex("#3e372d"), mute: ui.Hex("#7f7461"), faint: ui.Hex("#bcb098"),
		hair: ui.RGBA(27, 23, 18, .2), hair2: ui.RGBA(27, 23, 18, .09), hover: ui.RGBA(27, 23, 18, .04),
		signal: ui.Hex("#b3261e"), good: ui.Hex("#2f6b3a"), warn: ui.Hex("#a8700c"),
		route: map[string]ui.Color{"default": ui.Hex("#1f3f74"), "ai": ui.Hex("#6b3d7a"), "media": ui.Hex("#b3261e"), "dev": ui.Hex("#2b6a66"), "direct": ui.Hex("#8a8170")},
		font:  sans, display: faceSerif, round: .3, deco: decoBroadsheet,
	}
	broadsheetDark = pal{
		dark:  true,
		paper: ui.Hex("#1a1814"), paper2: ui.Hex("#201d18"), card: ui.Hex("#24211b"), sheet: ui.Hex("#2a261f"),
		ink: ui.Hex("#efe6d0"), ink2: ui.Hex("#cdc3ab"), mute: ui.Hex("#8f8670"), faint: ui.Hex("#4e483b"),
		hair: ui.RGBA(239, 230, 208, .18), hair2: ui.RGBA(239, 230, 208, .08), hover: ui.RGBA(239, 230, 208, .04),
		signal: ui.Hex("#e0533f"), good: ui.Hex("#6fb87a"), warn: ui.Hex("#e2a540"),
		route: map[string]ui.Color{"default": ui.Hex("#7d9bd1"), "ai": ui.Hex("#b48ac4"), "media": ui.Hex("#e0705f"), "dev": ui.Hex("#66b3a9"), "direct": ui.Hex("#857c69")},
		font:  sans, display: faceSerif, round: .3, deco: decoBroadsheet,
	}

	phosphorPal = pal{
		dark:  true,
		paper: ui.Hex("#050b07"), paper2: ui.Hex("#08120b"), card: ui.Hex("#0b170e"), sheet: ui.Hex("#0e1d12"),
		ink: ui.Hex("#a6ffbc"), ink2: ui.Hex("#74dc8f"), mute: ui.Hex("#3f8f55"), faint: ui.Hex("#1e4a2a"),
		hair: ui.RGBA(110, 255, 150, .2), hair2: ui.RGBA(110, 255, 150, .09), hover: ui.RGBA(110, 255, 150, .05),
		signal: ui.Hex("#ff5f4a"), good: ui.Hex("#5dff8a"), warn: ui.Hex("#ffc24a"),
		route: map[string]ui.Color{"default": ui.Hex("#46f08a"), "ai": ui.Hex("#f5c542"), "media": ui.Hex("#ff7a59"), "dev": ui.Hex("#39d5ff"), "direct": ui.Hex("#3f8f55")},
		font:  faceMono, display: faceMono, round: 0, glow: ui.RGBA(70, 240, 138, .55), deco: decoPhosphor,
	}

	neonPal = pal{
		dark:  true,
		paper: ui.Hex("#0d0a1f"), paper2: ui.Hex("#130f2b"), card: ui.Hex("#171233"), sheet: ui.Hex("#1c163f"),
		ink: ui.Hex("#f6f2ff"), ink2: ui.Hex("#cbc2f2"), mute: ui.Hex("#857cb6"), faint: ui.Hex("#3b3468"),
		hair: ui.RGBA(190, 160, 255, .18), hair2: ui.RGBA(190, 160, 255, .09), hover: ui.RGBA(190, 160, 255, .06),
		signal: ui.Hex("#ff3b6b"), good: ui.Hex("#2bffc6"), warn: ui.Hex("#ffd23f"),
		route: map[string]ui.Color{"default": ui.Hex("#3bd8ff"), "ai": ui.Hex("#b46bff"), "media": ui.Hex("#ff3fa4"), "dev": ui.Hex("#2bffc6"), "direct": ui.Hex("#6c6699")},
		font:  sans, display: faceFutura, round: 1.3, glow: ui.RGBA(255, 63, 164, .6), deco: decoNeon,
	}

	blueprintPal = pal{
		dark:  true,
		paper: ui.Hex("#0e3363"), paper2: ui.Hex("#113a6e"), card: ui.Hex("#123e76"), sheet: ui.Hex("#15457f"),
		ink: ui.Hex("#f0f7ff"), ink2: ui.Hex("#c7def6"), mute: ui.Hex("#86abd4"), faint: ui.Hex("#3f6ca3"),
		hair: ui.RGBA(220, 236, 255, .26), hair2: ui.RGBA(220, 236, 255, .11), hover: ui.RGBA(220, 236, 255, .06),
		signal: ui.Hex("#ff7a59"), good: ui.Hex("#7dffb3"), warn: ui.Hex("#ffd166"),
		route: map[string]ui.Color{"default": ui.Hex("#ffffff"), "ai": ui.Hex("#ffd166"), "media": ui.Hex("#ff8fa3"), "dev": ui.Hex("#7de3ff"), "direct": ui.Hex("#86abd4")},
		font:  sans, display: faceDIN, round: .25, deco: decoBlueprint,
	}

	sakuraLight = pal{
		paper: ui.Hex("#fbf2f0"), paper2: ui.Hex("#f6e7e4"), card: ui.Hex("#fff9f7"), sheet: ui.Hex("#fffbfa"),
		ink: ui.Hex("#2e1c23"), ink2: ui.Hex("#5c4049"), mute: ui.Hex("#a1838c"), faint: ui.Hex("#dcc2c8"),
		hair: ui.RGBA(120, 50, 70, .13), hair2: ui.RGBA(120, 50, 70, .065), hover: ui.RGBA(120, 50, 70, .035),
		signal: ui.Hex("#de4358"), good: ui.Hex("#3a9a6c"), warn: ui.Hex("#d18a24"),
		route: map[string]ui.Color{"default": ui.Hex("#6c7fd8"), "ai": ui.Hex("#a468d6"), "media": ui.Hex("#e45a8a"), "dev": ui.Hex("#35a6a1"), "direct": ui.Hex("#b49aa1")},
		font:  sans, display: faceRounded, round: 1.6, deco: decoSakura,
	}
	sakuraDark = pal{
		dark:  true,
		paper: ui.Hex("#1e1419"), paper2: ui.Hex("#24181e"), card: ui.Hex("#2a1c23"), sheet: ui.Hex("#302029"),
		ink: ui.Hex("#f8e9ee"), ink2: ui.Hex("#dcc3cc"), mute: ui.Hex("#a08590"), faint: ui.Hex("#56404a"),
		hair: ui.RGBA(255, 200, 220, .14), hair2: ui.RGBA(255, 200, 220, .07), hover: ui.RGBA(255, 200, 220, .045),
		signal: ui.Hex("#ff6b7f"), good: ui.Hex("#5fd39a"), warn: ui.Hex("#f2b04a"),
		route: map[string]ui.Color{"default": ui.Hex("#8e9cff"), "ai": ui.Hex("#c792ff"), "media": ui.Hex("#ff7fae"), "dev": ui.Hex("#4fd1c9"), "direct": ui.Hex("#8a7480")},
		font:  sans, display: faceRounded, round: 1.6, deco: decoSakura,
	}
)

// The second set: 山水 and 暮色, then 青瓷, 敦煌, 极光, 黑曜 and 孔版.
var (
	shanshuiLight = pal{
		paper: ui.Hex("#f1eee4"), paper2: ui.Hex("#e9e5d9"), card: ui.Hex("#f7f5ed"), sheet: ui.Hex("#faf8f1"),
		ink: ui.Hex("#1a1a18"), ink2: ui.Hex("#3d3d39"), mute: ui.Hex("#85837a"), faint: ui.Hex("#c2bfb3"),
		hair: ui.RGBA(26, 26, 24, .13), hair2: ui.RGBA(26, 26, 24, .065), hover: ui.RGBA(26, 26, 24, .035),
		signal: ui.Hex("#c8372d"), good: ui.Hex("#3f8468"), warn: ui.Hex("#a67c1a"),
		route: map[string]ui.Color{"default": ui.Hex("#3a5f8a"), "ai": ui.Hex("#6f5a8c"), "media": ui.Hex("#b5495b"), "dev": ui.Hex("#3f8f7a"), "direct": ui.Hex("#96948a")},
		font:  sans, display: faceSerif, round: .8, deco: decoShanshui,
	}
	shanshuiDark = pal{
		dark:  true,
		paper: ui.Hex("#121413"), paper2: ui.Hex("#171918"), card: ui.Hex("#1b1d1c"), sheet: ui.Hex("#202322"),
		ink: ui.Hex("#ebe8de"), ink2: ui.Hex("#c8c5ba"), mute: ui.Hex("#86857c"), faint: ui.Hex("#454743"),
		hair: ui.RGBA(235, 232, 222, .13), hair2: ui.RGBA(235, 232, 222, .065), hover: ui.RGBA(235, 232, 222, .04),
		signal: ui.Hex("#e85a4c"), good: ui.Hex("#6dbb99"), warn: ui.Hex("#d9ad4c"),
		route: map[string]ui.Color{"default": ui.Hex("#7fa3d1"), "ai": ui.Hex("#a995cc"), "media": ui.Hex("#e0808f"), "dev": ui.Hex("#6fc4ad"), "direct": ui.Hex("#7a7a72")},
		font:  sans, display: faceSerif, round: .8, deco: decoShanshui,
	}
	duskPal = pal{
		dark:  true,
		paper: ui.Hex("#15132b"), paper2: ui.Hex("#1a1733"), card: ui.Hex("#1f1b3a"), sheet: ui.Hex("#252043"),
		ink: ui.Hex("#f7efe9"), ink2: ui.Hex("#d6c9cf"), mute: ui.Hex("#9486a3"), faint: ui.Hex("#453c63"),
		hair: ui.RGBA(255, 225, 215, .15), hair2: ui.RGBA(255, 225, 215, .075), hover: ui.RGBA(255, 225, 215, .05),
		signal: ui.Hex("#ff6b6b"), good: ui.Hex("#7fe0b0"), warn: ui.Hex("#ffc66b"),
		route: map[string]ui.Color{"default": ui.Hex("#8fb0ff"), "ai": ui.Hex("#c9a0ff"), "media": ui.Hex("#ff8fb0"), "dev": ui.Hex("#6fe0d0"), "direct": ui.Hex("#7d7296")},
		font:  sans, display: faceSerif, round: 1.2, deco: decoDusk,
	}
)

var (
	celadonLight = pal{
		paper: ui.Hex("#e8efe8"), paper2: ui.Hex("#dee7de"), card: ui.Hex("#f1f6f0"), sheet: ui.Hex("#f6faf5"),
		ink: ui.Hex("#14231d"), ink2: ui.Hex("#33483f"), mute: ui.Hex("#6c8277"), faint: ui.Hex("#b3c4b8"),
		hair: ui.RGBA(20, 35, 29, .13), hair2: ui.RGBA(20, 35, 29, .065), hover: ui.RGBA(20, 35, 29, .035),
		signal: ui.Hex("#c4452f"), good: ui.Hex("#2f8a5c"), warn: ui.Hex("#b9801a"),
		route: map[string]ui.Color{"default": ui.Hex("#2f6f9e"), "ai": ui.Hex("#7a5aa8"), "media": ui.Hex("#c4567a"), "dev": ui.Hex("#1f8f86"), "direct": ui.Hex("#8d9b92")},
		font:  sans, display: faceSerif, round: 1.25, deco: decoCeladon,
	}
	celadonDark = pal{
		dark:  true,
		paper: ui.Hex("#0f1a17"), paper2: ui.Hex("#13201c"), card: ui.Hex("#172621"), sheet: ui.Hex("#1b2c26"),
		ink: ui.Hex("#e4f0e9"), ink2: ui.Hex("#bcd0c5"), mute: ui.Hex("#7d968a"), faint: ui.Hex("#3b5148"),
		hair: ui.RGBA(200, 235, 215, .13), hair2: ui.RGBA(200, 235, 215, .065), hover: ui.RGBA(200, 235, 215, .04),
		signal: ui.Hex("#f0705a"), good: ui.Hex("#5fcf98"), warn: ui.Hex("#e6b04e"),
		route: map[string]ui.Color{"default": ui.Hex("#6fb0dc"), "ai": ui.Hex("#b094dc"), "media": ui.Hex("#ee88a8"), "dev": ui.Hex("#4fd0c2"), "direct": ui.Hex("#6f857a")},
		font:  sans, display: faceSerif, round: 1.25, deco: decoCeladon,
	}

	dunhuangLight = pal{
		paper: ui.Hex("#ead9bb"), paper2: ui.Hex("#e2cfae"), card: ui.Hex("#f1e3c8"), sheet: ui.Hex("#f5e9d2"),
		ink: ui.Hex("#2a1a12"), ink2: ui.Hex("#4d3526"), mute: ui.Hex("#876c54"), faint: ui.Hex("#c4ad8a"),
		hair: ui.RGBA(42, 26, 18, .18), hair2: ui.RGBA(42, 26, 18, .085), hover: ui.RGBA(42, 26, 18, .04),
		signal: ui.Hex("#b8321f"), good: ui.Hex("#2f7a5e"), warn: ui.Hex("#b0700c"),
		route: map[string]ui.Color{"default": ui.Hex("#24528f"), "ai": ui.Hex("#7a4a8a"), "media": ui.Hex("#c2513a"), "dev": ui.Hex("#1f7f72"), "direct": ui.Hex("#96826a")},
		font:  sans, display: faceSerif, round: .6, deco: decoDunhuang,
	}
	dunhuangDark = pal{
		dark:  true,
		paper: ui.Hex("#1c1410"), paper2: ui.Hex("#231914"), card: ui.Hex("#291d17"), sheet: ui.Hex("#30231b"),
		ink: ui.Hex("#f0dfc0"), ink2: ui.Hex("#d2bd99"), mute: ui.Hex("#97826a"), faint: ui.Hex("#54433a"),
		hair: ui.RGBA(240, 223, 192, .16), hair2: ui.RGBA(240, 223, 192, .075), hover: ui.RGBA(240, 223, 192, .04),
		signal: ui.Hex("#e8604a"), good: ui.Hex("#5fbf9c"), warn: ui.Hex("#e6aa48"),
		route: map[string]ui.Color{"default": ui.Hex("#6f9be0"), "ai": ui.Hex("#b98acb"), "media": ui.Hex("#ee7a62"), "dev": ui.Hex("#4fc4b0"), "direct": ui.Hex("#86735f")},
		font:  sans, display: faceSerif, round: .6, deco: decoDunhuang,
	}

	auroraPal = pal{
		dark:  true,
		paper: ui.Hex("#0a1220"), paper2: ui.Hex("#0e1828"), card: ui.Hex("#111d30"), sheet: ui.Hex("#152338"),
		ink: ui.Hex("#e8f1fb"), ink2: ui.Hex("#bccbdf"), mute: ui.Hex("#7389a6"), faint: ui.Hex("#2f405a"),
		hair: ui.RGBA(180, 215, 255, .15), hair2: ui.RGBA(180, 215, 255, .075), hover: ui.RGBA(180, 215, 255, .05),
		signal: ui.Hex("#ff6b7a"), good: ui.Hex("#5ff0b0"), warn: ui.Hex("#ffcf6b"),
		route: map[string]ui.Color{"default": ui.Hex("#6cb6ff"), "ai": ui.Hex("#b79cff"), "media": ui.Hex("#ff8fc0"), "dev": ui.Hex("#4ff0d0"), "direct": ui.Hex("#5f7391")},
		font:  sans, display: faceDIN, round: 1.1, glow: ui.RGBA(95, 240, 176, .28), deco: decoAurora,
	}

	obsidianPal = pal{
		dark:  true,
		paper: ui.Hex("#050505"), paper2: ui.Hex("#0a0a0a"), card: ui.Hex("#0f0f0e"), sheet: ui.Hex("#151514"),
		ink: ui.Hex("#f5f5f2"), ink2: ui.Hex("#c9c9c4"), mute: ui.Hex("#7c7c77"), faint: ui.Hex("#3a3a37"),
		hair: ui.RGBA(255, 255, 255, .14), hair2: ui.RGBA(255, 255, 255, .07), hover: ui.RGBA(255, 255, 255, .05),
		signal: ui.Hex("#ff4d3d"), good: ui.Hex("#3ddc84"), warn: ui.Hex("#ffb020"),
		route: map[string]ui.Color{"default": ui.Hex("#5b9dff"), "ai": ui.Hex("#b18cff"), "media": ui.Hex("#ff6fa0"), "dev": ui.Hex("#2ad4d4"), "direct": ui.Hex("#6a6a66")},
		font:  sans, display: faceDIN, round: .5, deco: decoObsidian,
	}

	risoPal = pal{
		paper: ui.Hex("#f4efe3"), paper2: ui.Hex("#ebe5d6"), card: ui.Hex("#faf6ec"), sheet: ui.Hex("#fdfaf2"),
		ink: ui.Hex("#1b2466"), ink2: ui.Hex("#3a4388"), mute: ui.Hex("#7c83ad"), faint: ui.Hex("#c3c6da"),
		hair: ui.RGBA(27, 36, 102, .18), hair2: ui.RGBA(27, 36, 102, .085), hover: ui.RGBA(27, 36, 102, .04),
		signal: ui.Hex("#f0425a"), good: ui.Hex("#1f9d6b"), warn: ui.Hex("#d99200"),
		route: map[string]ui.Color{"default": ui.Hex("#2b46d6"), "ai": ui.Hex("#8a3fd1"), "media": ui.Hex("#ff48a0"), "dev": ui.Hex("#00a39a"), "direct": ui.Hex("#9a9fbd")},
		font:  sans, display: faceFutura, round: .8, deco: decoRiso,
	}
)

// skins are the looks to pick, in settings.Skins' order; the first is the
// default.
var skins = []skin{
	{id: "paper", zh: "纸墨", en: "Paper", noteZh: "暖白纸面与墨色，安静的报刊排版", noteEn: "Warm paper and ink, set quietly",
		light: &lightPal, dark: &darkPal, accent: "#DDFB4C"},
	{id: "broadsheet", zh: "报刊", en: "Broadsheet", noteZh: "新闻纸、衬线大标题与朱红", noteEn: "Newsprint, serif headlines and vermilion",
		light: &broadsheetLight, dark: &broadsheetDark, accent: "#B3261E"},
	{id: "phosphor", zh: "荧光屏", en: "Phosphor", noteZh: "绿色荧光的老终端：等宽字、扫描线、直角", noteEn: "An old green terminal: monospace, scan lines, square corners",
		dark: &phosphorPal, accent: "#46F08A"},
	{id: "neon", zh: "霓虹", en: "Neon", noteZh: "深夜紫底上发光的品红与青", noteEn: "Glowing magenta and cyan on midnight violet",
		dark: &neonPal, accent: "#FF3FA4"},
	{id: "blueprint", zh: "蓝图", en: "Blueprint", noteZh: "工程蓝底、网格与工程字体", noteEn: "Engineer's blue, a grid and drafting type",
		dark: &blueprintPal, accent: "#FFD166"},
	{id: "sakura", zh: "樱", en: "Sakura", noteZh: "樱花粉、圆润的角与飘落的花瓣", noteEn: "Blossom pink, soft corners and falling petals",
		light: &sakuraLight, dark: &sakuraDark, accent: "#E45A8A"},
	{id: "shanshui", zh: "山水", en: "Shanshui", noteZh: "宣纸、宋体与一方朱印，远山在雾里", noteEn: "Rice paper, serif type and a red seal, hills in the mist",
		light: &shanshuiLight, dark: &shanshuiDark, accent: "#C8372D"},
	{id: "celadon", zh: "青瓷", en: "Celadon", noteZh: "粉青釉色、衬线标题与细细的开片", noteEn: "Pale green glaze, serif headlines and a fine crackle",
		light: &celadonLight, dark: &celadonDark, accent: "#3E9C82"},
	{id: "dunhuang", zh: "敦煌", en: "Dunhuang", noteZh: "壁画的土黄、石青、石绿与朱砂", noteEn: "A mural's ochre, azurite, malachite and cinnabar",
		light: &dunhuangLight, dark: &dunhuangDark, accent: "#1E8573"},
	{id: "dusk", zh: "暮色", en: "Dusk", noteZh: "靛蓝的天沉向一线暖光", noteEn: "An indigo sky sinking to a line of warm light",
		dark: &duskPal, accent: "#FF9E7A"},
	{id: "aurora", zh: "极光", en: "Aurora", noteZh: "极夜深蓝上垂下的绿与紫，几点星", noteEn: "Curtains of green and violet over a polar night, and a few stars",
		dark: &auroraPal, accent: "#5FF0B0"},
	{id: "obsidian", zh: "黑曜", en: "Obsidian", noteZh: "纯黑、细线与一点余烬的橙", noteEn: "True black, hairlines and one ember of orange",
		dark: &obsidianPal, accent: "#FF7A1A"},
	{id: "riso", zh: "孔版", en: "Riso", noteZh: "孔版印刷：蓝墨、荧光粉与网点", noteEn: "Risograph: blue ink, fluorescent pink and halftone dots",
		light: &risoPal, accent: "#FF48A0"},
}

func skinByID(id string) *skin {
	for i := range skins {
		if skins[i].id == id {
			return &skins[i]
		}
	}
	return &skins[0]
}

// themeSkin is the skin the windows draw with, which each window's view
// sets (useLook) before it draws, as themeAccent.
var themeSkin = settings.Defaults.Skin

// useLook makes s's skin and theme color what is drawn next.
func useLook(s settings.Settings) {
	useAccent(s.Accent)
	themeSkin = s.Skin
}

// appearanceOf is the appearance the Mac draws the windows' frames in: the
// setting's, or the one the skin keeps.
func appearanceOf(s settings.Settings) string {
	if f := skinByID(s.Skin).fixed(); f != "" {
		return f
	}
	return s.Appearance
}

// skinPick sets skin id and its theme color.
func skinPick(s *settings.Settings, id string) {
	s.Skin, s.Accent = id, skinByID(id).accent
}

// r is a corner radius in p's skin: square, as drawn, or rounder.
func (p *pal) r(v float32) float32 { return v * p.round }

// baseRadius is the toolkit's own radius for controls.
var baseRadius = ui.LightTheme().Radius

// applyTheme makes p the toolkit's theme for what is drawn: its colors,
// its corners and, when font, its face.
func applyTheme(c *ui.Context, p *pal, font bool) {
	t := *c.Theme()
	t.Dark = p.dark
	t.Background, t.Text, t.TextMuted, t.Border = p.paper, p.ink, p.mute, p.hair
	t.Surface, t.SurfaceHover = p.card, p.paper2
	accentTheme(&t, p)
	t.Inverse, t.InverseText = p.ink, p.paper
	t.Radius = baseRadius * p.round
	if font {
		t.Font = p.font
	}
	c.SetTheme(&t)
}

// disp is big type in p's skin: its display face, and its glow.
func disp(e ui.Element, p *pal) ui.Element {
	if p.display != "" && p.display != p.font {
		e.Font(p.display)
	}
	if p.glow.A > 0 {
		e.TextShadow(0, 0, 12, p.glow)
	}
	return e
}

// dispIn is disp for big type in col, which may be dimmed: only type in
// ink, or another color that stands out on the paper, glows.
func dispIn(e ui.Element, p *pal, col ui.Color) ui.Element {
	if p.glow.A > 0 && col != p.ink && contrast(col, p.paper) < 4.5 {
		q := *p
		q.glow = ui.Color{}
		return disp(e, &q)
	}
	return disp(e, p)
}

// lit is what is filled with the accent, glowing in a skin that glows.
func lit(e ui.Element, p *pal) ui.Element {
	if p.glow.A > 0 {
		e.Shadow(0, 0, 14, 0, p.accent.Alpha(.5))
	}
	return e
}

// skinDeco lays p's texture over what is drawn before it in the element
// it is put in, taking no input and hidden from accessibility.
func skinDeco(c *ui.Context, p *pal) {
	if p.deco == nil {
		return
	}
	deco := p.deco
	ui.Box(c.Key("skin-deco")).Absolute().Fill().IgnoreInput(true).AccessHidden(true).
		Draw(func(pt *ui.Painter, r ui.Rect) { deco(pt, r, p) })
}

// vignette darkens r's edges with col, at alpha a.
func vignette(pt *ui.Painter, r ui.Rect, col ui.Color, a float32) {
	pt.FillRadialGradient(r, ui.RadialGradient{Stops: []ui.GradientStop{
		{Color: col.Alpha(0), At: 0}, {Color: col.Alpha(0), At: .55}, {Color: col.Alpha(a), At: 1},
	}}, 0)
}

// glowAt is a soft light of col at x, y, reaching rad.
func glowAt(pt *ui.Painter, x, y, rad float32, col ui.Color, a float32) {
	box := ui.Rect{X: x - rad, Y: y - rad, W: 2 * rad, H: 2 * rad}
	pt.FillRadialGradient(box, ui.RadialGradient{Radius: rad, Stops: []ui.GradientStop{
		{Color: col.Alpha(a), At: 0}, {Color: col.Alpha(a * .45), At: .4}, {Color: col.Alpha(0), At: 1},
	}}, 0)
}

// hash is a cheap, fixed scatter: the same specks in every frame.
func hash(i int) float32 {
	x := uint32(i)*2654435761 ^ 0x9e3779b9
	x ^= x >> 15
	x *= 2246822519
	x ^= x >> 13
	return float32(x%10000) / 10000
}

// decoBroadsheet: newsprint's grain, the edges browned.
func decoBroadsheet(pt *ui.Painter, r ui.Rect, p *pal) {
	speck := p.ink.Alpha(.07)
	if p.dark {
		speck = p.ink.Alpha(.05)
	}
	n := int(r.W * r.H / 520)
	q := pt.Scale()
	for i := 0; i < n; i++ {
		x, y := r.X+hash(2*i)*r.W, r.Y+hash(2*i+1)*r.H
		s := 1 / q * float32(1+int(hash(i+7)*2))
		pt.Fill(ui.Rect{X: x, Y: y, W: s, H: s}, speck, 0)
	}
	vignette(pt, r, ui.Hex("#5a3c14"), map[bool]float32{false: .14, true: .3}[p.dark])
}

// decoPhosphor: the tube's scan lines, its glow in the middle, its dark
// rounded edges.
func decoPhosphor(pt *ui.Painter, r ui.Rect, p *pal) {
	glowAt(pt, r.X+r.W/2, r.Y+r.H/2, max(r.W, r.H)*.6, p.ink, .05)
	line := ui.RGBA(0, 0, 0, .28)
	for y := r.Y; y < r.Y+r.H; y += 3 {
		pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 1}, line, 0)
	}
	vignette(pt, r, ui.Hex("#000000"), .55)
}

// decoNeon: two lights, magenta and cyan, from the corners, and a line of
// both along the top.
func decoNeon(pt *ui.Painter, r ui.Rect, p *pal) {
	big := max(r.W, r.H)
	glowAt(pt, r.X+r.W*.08, r.Y+r.H*.05, big*.55, p.route["media"], .16)
	glowAt(pt, r.X+r.W*.95, r.Y+r.H*.98, big*.55, p.route["default"], .13)
	glowAt(pt, r.X+r.W*.6, r.Y+r.H*.4, big*.3, p.route["ai"], .05)
	pt.FillGradient(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, ui.LinearGradient{From: p.route["media"], To: p.route["default"], Angle: 90}, 0)
}

// decoBlueprint: the drafting grid, fine and every fifth line bold, and
// registration ticks at the corners.
func decoBlueprint(pt *ui.Painter, r ui.Rect, p *pal) {
	const step = 16
	q := pt.Scale()
	w := 1 / q
	minor, major := p.ink.Alpha(.045), p.ink.Alpha(.1)
	for i := 0; float32(i)*step < r.W; i++ {
		col := minor
		if i%5 == 0 {
			col = major
		}
		pt.Fill(ui.Rect{X: r.X + float32(i)*step, Y: r.Y, W: w, H: r.H}, col, 0)
	}
	for i := 0; float32(i)*step < r.H; i++ {
		col := minor
		if i%5 == 0 {
			col = major
		}
		pt.Fill(ui.Rect{X: r.X, Y: r.Y + float32(i)*step, W: r.W, H: w}, col, 0)
	}
	tick := p.ink.Alpha(.45)
	for _, c := range [][2]float32{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		x, y := r.X+c[0]*r.W, r.Y+c[1]*r.H
		sx, sy := float32(1-2*c[0]), float32(1-2*c[1])
		x, y = x+sx*10, y+sy*10
		pt.Line(x, y, x+sx*14, y, 1.2, tick)
		pt.Line(x, y, x, y+sy*14, 1.2, tick)
	}
	vignette(pt, r, ui.Hex("#04142b"), .35)
}

// decoSakura: a blush of light at the top right, and petals falling
// across it.
func decoSakura(pt *ui.Painter, r ui.Rect, p *pal) {
	pink := p.route["media"]
	big := max(r.W, r.H)
	glowAt(pt, r.X+r.W*.92, r.Y+r.H*.02, big*.5, pink, map[bool]float32{false: .14, true: .1}[p.dark])
	n := max(4, int(r.W*r.H/30000))
	for i := 0; i < n; i++ {
		// Thicker toward the top right, where the blush is.
		u, v := hash(3*i), hash(3*i+1)
		x := r.X + (1-u*u)*r.W
		y := r.Y + v*v*r.H
		size := 5 + hash(3*i+2)*7
		a := .12 + .16*(1-v)
		petal(pt, x, y, size, float32(hash(i+11))*math.Pi*2, pink.Alpha(a))
	}
}

// petal draws one blossom petal at x, y, size long, turned by angle.
func petal(pt *ui.Painter, x, y, size, angle float32, col ui.Color) {
	cos, sin := float32(math.Cos(float64(angle))), float32(math.Sin(float64(angle)))
	at := func(px, py float32) (float32, float32) { return x + px*cos - py*sin, y + px*sin + py*cos }
	var path ui.Path
	l, w := size, size*.62
	path.MoveTo(at(0, 0))
	c1x, c1y := at(-w, -l*.45)
	c2x, c2y := at(-w*.5, -l)
	nx, ny := at(-w*.12, -l*.92)
	path.CubeTo(c1x, c1y, c2x, c2y, nx, ny)
	mx, my := at(0, -l*.8)
	path.LineTo(mx, my) // the notch at its tip
	ex, ey := at(w*.12, -l*.92)
	path.LineTo(ex, ey)
	c3x, c3y := at(w*.5, -l)
	c4x, c4y := at(w, -l*.45)
	ox, oy := at(0, 0)
	path.CubeTo(c3x, c3y, c4x, c4y, ox, oy)
	path.Close()
	pt.FillPath(&path, col)
}

// decoDunhuang: plaster's grain and its worn patches, the pigments'
// light from two corners, the edges browned.
func decoDunhuang(pt *ui.Painter, r ui.Rect, p *pal) {
	big := max(r.W, r.H)
	speck := p.ink.Alpha(map[bool]float32{false: .08, true: .05}[p.dark])
	q := pt.Scale()
	n := int(r.W * r.H / 420)
	for i := 0; i < n; i++ {
		x, y := r.X+hash(2*i)*r.W, r.Y+hash(2*i+1)*r.H
		s := 1 / q * float32(1+int(hash(i+7)*2))
		pt.Fill(ui.Rect{X: x, Y: y, W: s, H: s}, speck, 0)
	}
	patch, pa := ui.Hex("#fff6dc"), float32(.16)
	if p.dark {
		patch, pa = ui.Hex("#8a5a2e"), .07
	}
	m := int(r.W * r.H / 45000)
	for i := 0; i < m; i++ {
		glowAt(pt, r.X+hash(3*i+100)*r.W, r.Y+hash(3*i+101)*r.H, 30+hash(3*i+102)*70, patch, pa)
	}
	glowAt(pt, r.X+r.W, r.Y+r.H, big*.5, p.route["default"], .07)
	glowAt(pt, r.X, r.Y, big*.45, p.signal, .06)
	vignette(pt, r, ui.Hex("#4a2a10"), map[bool]float32{false: .16, true: .32}[p.dark])
}

// decoCeladon: the glaze, thin and bright where the light falls, pooled
// deeper at the edges, and its crackle.
func decoCeladon(pt *ui.Painter, r ui.Rect, p *pal) {
	big := max(r.W, r.H)
	if p.dark {
		glowAt(pt, r.X+r.W*.12, r.Y, big*.6, ui.Hex("#7fe0c0"), .08)
		vignette(pt, r, ui.Hex("#020806"), .45)
	} else {
		glowAt(pt, r.X+r.W*.12, r.Y, big*.6, ui.Hex("#ffffff"), .45)
		vignette(pt, r, ui.Hex("#3f9478"), .16)
	}
	line := p.ink.Alpha(.07)
	w := 1 / pt.Scale()
	n := int(r.W * r.H / 20000)
	for i := 0; i < n; i++ {
		x, y := r.X+hash(5*i)*r.W, r.Y+hash(5*i+1)*r.H
		ang := hash(5*i+2) * math.Pi * 2
		pts := [][2]float32{{x, y}}
		for j := 0; j < 6; j++ {
			ang += (hash(5*i+3+j*13) - .5) * 1.7
			l := 22 + hash(5*i+4+j*29)*46
			x += l * float32(math.Cos(float64(ang)))
			y += l * float32(math.Sin(float64(ang)))
			pts = append(pts, [2]float32{x, y})
		}
		pt.StrokePolyline(pts, w, line)
	}
}

// decoAurora: curtains of light hanging from the top, rays in each, and
// stars.
func decoAurora(pt *ui.Painter, r ui.Rect, p *pal) {
	cols := []ui.Color{p.good, p.route["dev"], p.route["ai"]}
	const step = 3
	for k, col := range cols {
		fk := float64(k)
		for x := float32(0); x < r.W; x += step {
			f := float64(x / r.W)
			// Where the curtain hangs, and how bright this ray of it is.
			top := r.H * float32(.03+.05*fk+.05*math.Sin(f*5.5+fk*2.1))
			fold := .5 + .5*math.Sin(f*9+fk*1.7+2*math.Sin(f*4+fk))
			ray := .6 + .4*math.Sin(f*140+fk*3+6*math.Sin(f*17))
			a := float32(fold * fold * ray * (.15 - .03*fk))
			h := r.H * float32(.16+.2*fold)
			pt.FillGradient(ui.Rect{X: r.X + x, Y: r.Y + top, W: step, H: h}, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
				{Color: col.Alpha(0), At: 0}, {Color: col.Alpha(a), At: .18}, {Color: col.Alpha(a * .35), At: .55}, {Color: col.Alpha(0), At: 1},
			}}, 0)
		}
	}
	q := pt.Scale()
	n := int(r.W * r.H / 7000)
	for i := 0; i < n; i++ {
		v := hash(3*i + 1)
		x, y := r.X+hash(3*i)*r.W, r.Y+v*v*r.H
		s := 1 / q * float32(1+int(hash(3*i+2)*2.2))
		pt.Fill(ui.Rect{X: x, Y: y, W: s, H: s}, p.ink.Alpha(.18+.3*hash(i+5)*(1-v)), s/2)
	}
	vignette(pt, r, ui.Hex("#02060c"), .4)
}

// decoObsidian: two facets catching light, and an ember at the bottom
// left.
func decoObsidian(pt *ui.Painter, r ui.Rect, p *pal) {
	white := ui.Hex("#ffffff")
	var a, b ui.Path
	a.MoveTo(r.X+r.W*.52, r.Y)
	a.LineTo(r.X+r.W, r.Y)
	a.LineTo(r.X+r.W, r.Y+r.H*.62)
	a.Close()
	pt.FillPathGradient(&a, ui.LinearGradient{From: white.Alpha(.055), To: white.Alpha(.01), Angle: 225})
	pt.StrokePolyline([][2]float32{{r.X + r.W*.52, r.Y}, {r.X + r.W, r.Y + r.H*.62}}, 1, white.Alpha(.08))
	b.MoveTo(r.X, r.Y+r.H*.45)
	b.LineTo(r.X+r.W*.42, r.Y+r.H)
	b.LineTo(r.X, r.Y+r.H)
	b.Close()
	pt.FillPathGradient(&b, ui.LinearGradient{From: white.Alpha(.03), To: white.Alpha(.005), Angle: 180})
	pt.StrokePolyline([][2]float32{{r.X, r.Y + r.H*.45}, {r.X + r.W*.42, r.Y + r.H}}, 1, white.Alpha(.05))
	glowAt(pt, r.X+r.W*.04, r.Y+r.H*1.04, max(r.W, r.H)*.42, ui.Hex("#ff7a1a"), .15)
}

// decoRiso: halftone dots from the two bottom corners, pink and blue,
// thinning as they go.
func decoRiso(pt *ui.Painter, r ui.Rect, p *pal) {
	const pitch = 9
	dots := func(cx, cy, reach float32, col ui.Color) {
		for gy := 0; float32(gy)*pitch < r.H; gy++ {
			for gx := 0; float32(gx)*pitch < r.W; gx++ {
				x := r.X + float32(gx)*pitch + float32(gy%2)*pitch/2
				y := r.Y + float32(gy)*pitch
				d := float32(math.Hypot(float64(x-cx), float64(y-cy))) / reach
				if d >= 1 {
					continue
				}
				s := (1 - d) * (1 - d) * 6.5
				if s < .6 {
					continue
				}
				pt.Fill(ui.Rect{X: x - s/2, Y: y - s/2, W: s, H: s}, col, s/2)
			}
		}
	}
	dots(r.X+r.W, r.Y+r.H, min(r.W, r.H)*.4, p.route["media"].Alpha(.26))
	dots(r.X, r.Y+r.H, min(r.W, r.H)*.36, p.route["default"].Alpha(.2))
}

// decoShanshui: ranges of hills along the bottom, each nearer one darker,
// each fading into the mist below it; and the paper's fibers.
func decoShanshui(pt *ui.Painter, r ui.Rect, p *pal) {
	wash := p.ink
	w := 1 / pt.Scale()
	n := int(r.W * r.H / 9000)
	for i := 0; i < n; i++ {
		x, y := r.X+hash(4*i)*r.W, r.Y+hash(4*i+1)*r.H
		ang := float64(hash(4*i+2)) * math.Pi
		l := 6 + hash(4*i+3)*14
		pt.Line(x, y, x+l*float32(math.Cos(ang)), y+l*float32(math.Sin(ang)), w, wash.Alpha(.035))
	}
	const layers = 4
	for k := 0; k < layers; k++ {
		fk := float64(k)
		base := r.Y + r.H*float32(.60+.085*fk)
		amp := r.H * float32(.085-.008*fk)
		var path ui.Path
		path.MoveTo(r.X, r.Y+r.H)
		top := r.Y + r.H
		for x := float32(0); x <= r.W+4; x += 4 {
			f := float64(x/r.W)*(1.6+.5*fk) + fk*3.7
			h := .55*math.Sin(f*2.1+1.3*fk) + .28*math.Sin(f*5.3+fk) + .12*math.Sin(f*11.7+2*fk) + .05*math.Sin(f*29)
			// Peaks, not waves: sharpen the crests.
			h = math.Copysign(math.Pow(math.Abs(h), .8), h)
			y := base - amp*float32(h)
			top = min(top, y)
			path.LineTo(r.X+x, y)
		}
		path.LineTo(r.X+r.W, r.Y+r.H)
		path.Close()
		a := float32(.05 + .028*fk)
		if p.dark {
			a *= .8
		}
		// The gradient runs over the path's own box, top to bottom.
		pt.FillPathGradient(&path, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
			{Color: wash.Alpha(a), At: 0}, {Color: wash.Alpha(a * .5), At: .35}, {Color: wash.Alpha(0), At: .8},
		}})
	}
}

// decoDusk: the sky's last light: warm along the bottom, rose above it,
// the sun just under the edge.
func decoDusk(pt *ui.Painter, r ui.Rect, p *pal) {
	rose, amber := ui.Hex("#ff5f8f"), ui.Hex("#ffb060")
	pt.FillGradient(r, ui.LinearGradient{Angle: 180, Oklab: true, Stops: []ui.GradientStop{
		{Color: rose.Alpha(0), At: 0}, {Color: rose.Alpha(0), At: .35}, {Color: rose.Alpha(.1), At: .75}, {Color: amber.Alpha(.2), At: 1},
	}}, 0)
	glowAt(pt, r.X+r.W*.3, r.Y+r.H*1.12, max(r.W, r.H)*.55, amber, .22)
	glowAt(pt, r.X+r.W*.85, r.Y, max(r.W, r.H)*.4, ui.Hex("#5a6bff"), .1)
	q := pt.Scale()
	n := int(r.W * r.H / 11000)
	for i := 0; i < n; i++ {
		v := hash(3*i + 1)
		x, y := r.X+hash(3*i)*r.W, r.Y+v*v*r.H*.6
		s := 1 / q * float32(1+int(hash(3*i+2)*2.2))
		pt.Fill(ui.Rect{X: x, Y: y, W: s, H: s}, p.ink.Alpha(.15+.3*hash(i+5)*(1-v)), s/2)
	}
}
