# Silt dictionaries

Hunspell-format language packs and domain word-list supplements for inline
spellcheck (#196, #336, #337). Loaded by
`frontend/src/lib/editor/spellcheck/dictionary.ts`.

## Layout

```
dictionaries/
  <lang>/                    # BCP-47-ish tag = editor.spellcheck_language
    index.aff                # Hunspell affix rules
    index.dic                # Hunspell word list
  supplements/
    software-terms.txt       # Curated MIT software terms (default ON)
```

## Strategy: package defaults, download the rest

| Asset | Strategy |
|-------|----------|
| **en-US** | Bundled here (~605 KB). Offline day-one. |
| **Other languages** (en-GB, de, fr, es, …) | Download on demand into user-global cache (`UserConfigDir/silt/dictionaries/languages/`). |
| **software-terms** | Bundled curated subset; enabled by default via `editor.spellcheck_domains`. |
| **Other domain packs** | Download on demand into `…/dictionaries/domains/`. |

### Download source (not GitHub raw)

Language packs come from **version-pinned npm packages** published by
[wooorm/dictionaries](https://github.com/wooorm/dictionaries), fetched via
**jsDelivr**:

```
https://cdn.jsdelivr.net/npm/dictionary-en-gb@3.0.0/index.aff
https://cdn.jsdelivr.net/npm/dictionary-en-gb@3.0.0/index.dic
https://cdn.jsdelivr.net/npm/dictionary-en-gb@3.0.0/license
```

Domain packs come from **@cspell/dict-*** packages the same way. There is no
GitHub Releases CDN for these packages — do not use `raw.githubusercontent.com`
in production.

Catalog (IDs, pinned versions, licenses) lives in `backend/spellcheck/catalog.go`.

## Licenses

- **Engine**: [typo-js](https://github.com/cfinke/Typo.js) (Modified BSD)
- **en-US / en-GB**: MIT AND BSD (wooorm/dictionaries)
- **de**: GPL-2.0 OR GPL-3.0 — license file cached with the pack
- **fr**: MPL-2.0 — license file cached with the pack
- **es**: GPL-3.0 OR LGPL-3.0 OR MPL-1.1 — license file cached with the pack
- **Domain packs (v1)**: MIT only (software-terms, typescript, python, data-science)

AGPL and GPL medical packs are excluded from the v1 catalog.

## Privacy

Spellcheck is local: **note text never leaves the machine**. Optional downloads
fetch only dictionary *assets* when the user selects a non-bundled pack.
After the first download, packs work offline from the user-global cache.
