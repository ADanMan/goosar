#!/usr/bin/env python3
"""Замер сходства server2/** с server/** (эпик E8, clean-room).

Нормализация: убираются комментарии (//, /* */, -- в SQL, # в shell/yaml)
и пустые строки, пробелы по краям строк, слова goosar/multica (любой регистр)
заменяются на X. Сравнение построчное, difflib.SequenceMatcher.ratio().

Для каждого файла server2/** ищется самый похожий файл server/** с тем же
расширением. Код выхода 1, если хотя бы один файл >= порога (по умолчанию 0.30).

Запуск: python3 scripts/similarity-check.py [--threshold 0.30] [--top 20]

--ignore-trivial: дополнительно отбрасывает шаблонные строки (короче 12
символов: скобки, `return err`, `if err != nil {` и т.п.). Это справочный
режим: на коротких Go-файлах такие строки дают фон 25–30% даже против
файлов с другим назначением. Порог приёмки считается в основном режиме.
"""
import argparse
import difflib
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
EXTS = {".go", ".sql", ".yaml", ".yml", ".sh", ".toml", ".json", ".md"}
SKIP_DIRS = {"node_modules", ".git", "vendor", "testdata"}
NAME_RE = re.compile(r"goosar|multica", re.IGNORECASE)
BLOCK_RE = re.compile(r"/\*.*?\*/", re.DOTALL)


IGNORE_TRIVIAL = False
TRIVIAL = {"if err != nil {", "return nil, err", "return err", "return nil"}


def normalize(path):
    ext = os.path.splitext(path)[1]
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            text = f.read()
    except OSError:
        return []
    if ext in (".go", ".sql"):
        text = BLOCK_RE.sub("", text)
    out = []
    for line in text.splitlines():
        s = line.strip()
        if ext == ".go" and s.startswith("//"):
            continue
        if ext == ".sql" and s.startswith("--"):
            continue
        if ext in (".sh", ".yaml", ".yml", ".toml") and s.startswith("#"):
            continue
        if ext == ".go" and "//" in s and '"' not in s and "`" not in s:
            s = s.split("//", 1)[0].rstrip()
        if not s:
            continue
        if IGNORE_TRIVIAL and (len(s) < 12 or s in TRIVIAL):
            continue
        out.append(NAME_RE.sub("X", s))
    return out


def collect(base):
    files = []
    for dirpath, dirnames, filenames in os.walk(base):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for name in filenames:
            if os.path.splitext(name)[1] in EXTS and name != "go.sum":
                files.append(os.path.join(dirpath, name))
    return files


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--threshold", type=float, default=0.30)
    ap.add_argument("--top", type=int, default=15)
    ap.add_argument("--new", default=os.path.join(ROOT, "server2"))
    ap.add_argument("--old", default=os.path.join(ROOT, "server"))
    ap.add_argument("--ignore-trivial", action="store_true")
    args = ap.parse_args()
    global IGNORE_TRIVIAL
    IGNORE_TRIVIAL = args.ignore_trivial

    old = {}
    for p in collect(args.old):
        lines = normalize(p)
        if lines:
            old.setdefault(os.path.splitext(p)[1], []).append((p, lines, set(lines)))

    results = []
    for p in collect(args.new):
        a = normalize(p)
        if not a:
            continue
        aset = set(a)
        best, best_path = 0.0, ""
        for q, b, bset in old.get(os.path.splitext(p)[1], []):
            # верхняя граница ratio по мультимножествам — быстрый отсев
            common = len(aset & bset)
            if common == 0:
                continue
            upper = 2.0 * min(len(a), len(b)) / (len(a) + len(b))
            if upper <= best:
                continue
            sm = difflib.SequenceMatcher(None, a, b, autojunk=False)
            if sm.quick_ratio() <= best:
                continue
            r = sm.ratio()
            if r > best:
                best, best_path = r, q
        results.append((best, os.path.relpath(p, ROOT), os.path.relpath(best_path, ROOT) if best_path else "-"))

    results.sort(reverse=True)
    bad = [r for r in results if r[0] >= args.threshold]
    print(f"Файлов server2: {len(results)}; порог {args.threshold:.0%}; нарушений: {len(bad)}")
    print(f"{'сходство':>9}  {'server2':<60} ближайший в server/")
    for r, p, q in results[: args.top]:
        mark = "  <-- FAIL" if r >= args.threshold else ""
        print(f"{r:>8.1%}  {p:<60} {q}{mark}")
    if results:
        print(f"Максимум: {results[0][0]:.1%}")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
