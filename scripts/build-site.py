#!/usr/bin/env python3
"""Builds the website from site/ into build/site/: the Chinese page at /
and the English one at /en/, each holding one language only.

site/index.html carries both languages, three ways:
  <span class="zh">…</span><span class="en">…</span>   text
  <!--zh-->…<!--/zh-->  <!--en-->…<!--/en-->          blocks, in <head>
  attr="…" en:attr="…"                                 attributes, Chinese first
Opened as it is, it shows the Chinese page.
"""
import re
import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "site"
OUT = ROOT / "build" / "site"
LANGS = ("zh", "en")

SPAN = {l: re.compile(r'<span class="%s">(.*?)</span>' % l, re.S) for l in LANGS}
BLOCK = {l: re.compile(r"[ \t]*<!--%s-->\n?(.*?)<!--/%s-->\n?" % (l, l), re.S) for l in LANGS}
TAG_WITH_EN = re.compile(r"<[a-zA-Z][^<>]*\sen:[^<>]*>")
EN_ATTR = re.compile(r'\sen:([\w-]+)="([^"]*)"')
RELATIVE = re.compile(r'\b(src|href)="(?!#|/|\.\./|[a-z][a-z0-9+.-]*:)([^"]+)"')


def en_attrs(tag, lang):
    """Drops a tag's en: attributes, and in English puts them over the plain ones."""
    pairs = EN_ATTR.findall(tag)
    tag = EN_ATTR.sub("", tag)
    if lang != "en":
        return tag
    for name, value in pairs:
        plain = re.compile(r'(\s)%s="[^"]*"' % re.escape(name))
        if not plain.search(tag):
            sys.exit("build-site: en:%s has no %s to replace in %s" % (name, name, tag))
        tag = plain.sub(lambda m: '%s%s="%s"' % (m.group(1), name, value), tag, count=1)
    return tag


def render(html, lang):
    other = "en" if lang == "zh" else "zh"
    for l in LANGS:
        for m in SPAN[l].finditer(html):
            if "<span" in m.group(1):
                sys.exit("build-site: a %s span holds another span: %s" % (l, m.group(0)[:80]))
    html = SPAN[other].sub("", html)
    html = SPAN[lang].sub(lambda m: m.group(1), html)
    html = BLOCK[other].sub("", html)
    html = BLOCK[lang].sub(lambda m: m.group(1), html)
    html = TAG_WITH_EN.sub(lambda m: en_attrs(m.group(0), lang), html)
    if lang == "en":
        # The page sits one directory down, and shows the English screenshots.
        html = RELATIVE.sub(lambda m: '%s="../%s"' % (m.group(1), m.group(2)), html)
        html = re.sub(r"-zh(-dark)?\.webp", r"-en\1.webp", html)
    left = re.search(r'<span class="(zh|en)"|<!--/?(zh|en)-->|\sen:[\w-]+=', html)
    if left:
        sys.exit("build-site: %s page still has language markup: %s" % (lang, left.group(0)))
    return html


def main():
    if OUT.exists():
        shutil.rmtree(OUT)
    shutil.copytree(SRC, OUT)
    html = (SRC / "index.html").read_text(encoding="utf-8")
    (OUT / "index.html").write_text(render(html, "zh"), encoding="utf-8")
    (OUT / "en").mkdir()
    (OUT / "en" / "index.html").write_text(render(html, "en"), encoding="utf-8")
    print("built %s" % OUT.relative_to(ROOT))


if __name__ == "__main__":
    main()
