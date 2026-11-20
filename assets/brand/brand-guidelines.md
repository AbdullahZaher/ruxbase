# Ruxbase Brand Guidelines

Version: 1.1 | Language: English | Source: [Visual guidelines](brand-guidelines.html) (2026-11-20)

Machine-readable companion: [brand-tokens.json](brand-tokens.json). The HTML guide and existing master SVG assets are the source references; this document and JSON expose the same specifications for text search, developers and AI-assisted workflows. Asset links in this Markdown file are relative to this directory; JSON asset paths are relative to the repository root.

## 01 — Brand identity and design philosophy

**Ruxbase** is an open-source, PostgreSQL-native, self-hosted backend platform. Use **Ruxbase** in running text and **RUXBASE** in the wordmark.

Design style: modular geometric lettermark / neo-minimalism. Two geometric modules form an abstract uppercase R, with a central negative-space counter and a diagonal leg. The geometry expresses modular architecture, openness, control and scalability. The visual language is monochromatic, without decorative effects.

The symbol is a geometric reconstruction of the approved raster reference. Supporting identity specifications are proposed for approval.

## 02 — Logo variants and lockups

Use the standalone symbol for favicons and app icons, the horizontal lockup for website headers and navigation, and the vertical lockup for larger layouts.

| Variant | Light background | Dark background |
| --- | --- | --- |
| Symbol | [Black SVG](ruxbase-symbol-black.svg) | [White SVG](ruxbase-symbol-white.svg) |
| Horizontal lockup | [Black SVG](ruxbase-horizontal-black.svg) | [White SVG](ruxbase-horizontal-white.svg) |
| Vertical lockup | [Black SVG](ruxbase-stacked-black.svg) | [White SVG](ruxbase-stacked-white.svg) |

### Cross-theme outlined PNG variants

These supplied raster assets use white fill, black outlines and transparent backgrounds for both light and dark interfaces. They are distinct from the monochrome vector master.

| Asset | File |
| --- | --- |
| Full horizontal lockup | [ruxbase.png](ruxbase.png) |
| Icon only | [ruxbase_logo.png](ruxbase_logo.png) |
| Wordmark only | [ruxbase_name.png](ruxbase_name.png) |

## 03 — SVG geometry and construction grid

Master coordinates are symbol-local SVG user units unless specified. The icon canvas uses an offset of (150, 110).

| Property | Value |
| --- | --- |
| Reference unit U | 100 |
| Major / fine construction grid | 100 / 20 |
| Symbol bounds | 700 × 780 |
| Icon canvas | 1000 × 1000 |
| Icon offset | (150, 110) |
| First module bounds | 400 × 430 |
| Counter bounding box | 200 × 210 |
| Module contact points | (400, 220) and (200, 430) |
| Corner radii (upper-left, upper-right, middle-left, lower-left) | 75 / 30 / 50 / 75 |
| Diagonal leg | (380, 530) to (610, 780); approximately 47.4 degrees |
| Curve types | Cubic Bézier and circular arcs |
| Vertical lockup | Symbol width 700; wordmark width 790; vertical gap 120 |
| Horizontal lockup | Symbol height 280; wordmark width 711; gap approximately 74 |

[Construction grid SVG](ruxbase-construction-grid.svg) · [Construction grid PNG](ruxbase-construction-grid.png) · [Master icon SVG](ruxbase-icon-black.svg)

Exact path data from the existing master icon SVG:

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1000 1000" role="img" aria-label="Ruxbase">
  <g transform="translate(150 110)" fill="#000000">
    <path d="M75 0 H370 A30 30 0 0 1 400 30 V220 H200 V430 H50 A50 50 0 0 1 0 380 V75 A75 75 0 0 1 75 0 Z"/>
    <path d="M400 220 H560 C640 220 700 280 700 352 C700 450 626 530 550 530 H380 L610 780 H275 A75 75 0 0 1 200 705 V430 H300 C355.228 430 400 382.99 400 325 Z"/>
  </g>
</svg>
```

The coordinates describe the reconstructed SVG assets, not the outlined raster variants. Prefer the supplied production SVG files over redrawing the logo.

## 04 — Clear space and minimum sizes

Maintain minimum clear space of 1U = 100 SVG units (approximately 12.82% of symbol height) around the visible symbol bounds. Scale U proportionally with the symbol for complete lockups.

| Asset | Recommended digital minimum | Recommended print minimum |
| --- | --- | --- |
| Standalone symbol | 24 px high | 8 mm high |
| Horizontal lockup | 120 px wide | 30 mm wide |
| Vertical lockup | 100 px wide | 25 mm wide |
| Dedicated favicon | 16 px | Not applicable |

Use the dedicated 16 px favicon and verify legibility in context. These are recommended limits, not guarantees of legibility.

## 05 — Color palette

| Token | HEX | Role |
| --- | --- | --- |
| Carbon / Primary | #000000 | Master symbol |
| Pure White / Inverse | #FFFFFF | Inverse master symbol |
| Soft Canvas / Neutral | #F5F5F5 | Proposed UI surface |
| Muted / UI neutral | #737373 | Proposed UI neutral |

The master symbol uses black or white only. Gray colors are not additional logo colors. Keep semantic UI state colors separate from brand identity. Outlined PNGs use white fill and black outlines as a separate treatment.

## 06 — Typography and brand naming

- Inter Display Bold (700): reconstructed vector wordmark. Use the supplied outlined artwork.
- Inter (weights 400–700): English interfaces.
- Noto Sans Arabic: Arabic interfaces.
- Use RUXBASE in the official wordmark and Ruxbase in running text.

The original image's typeface is unknown; the reconstructed wordmark is a close approximation, not an exact typeface match. Supporting typography is proposed. “Build with control.” is an illustrative typography sample, not an approved marketing tagline.

## 07 — Logo usage rules

### Do

- Preserve proportions, module contact points and negative space.
- Use the inverse symbol on dark backgrounds, or use the supplied outlined PNG variants for cross-theme use.
- Test sizes on target screens before release.

### Do not

- Add custom outlines, gradients, shadows or glows to master artwork.
- Change corners or the diagonal leg.
- Stretch, rotate or place the logo on low-contrast backgrounds.

The supplied white-fill, black-outline PNG assets are a separate variant, not permission to modify the vector master.

## 08 — Brand asset inventory

| File | Asset type | Format and treatment |
| --- | --- | --- |
| [ruxbase.png](ruxbase.png) | Horizontal lockup | White fill, black outline; transparent PNG |
| [ruxbase_logo.png](ruxbase_logo.png) | Standalone symbol | White fill, black outline; transparent PNG |
| [ruxbase_name.png](ruxbase_name.png) | Standalone wordmark | White fill, black outline; transparent PNG |
| [ruxbase-symbol-black.svg](ruxbase-symbol-black.svg) | symbol — black | Transparent vector master |
| [ruxbase-symbol-white.svg](ruxbase-symbol-white.svg) | symbol — white | Transparent vector master |
| [ruxbase-icon-black.svg](ruxbase-icon-black.svg) | icon — black | Transparent vector master |
| [ruxbase-icon-white.svg](ruxbase-icon-white.svg) | icon — white | Transparent vector master |
| [ruxbase-stacked-black.svg](ruxbase-stacked-black.svg) | stacked — black | Transparent vector master |
| [ruxbase-stacked-white.svg](ruxbase-stacked-white.svg) | stacked — white | Transparent vector master |
| [ruxbase-horizontal-black.svg](ruxbase-horizontal-black.svg) | horizontal — black | Transparent vector master |
| [ruxbase-horizontal-white.svg](ruxbase-horizontal-white.svg) | horizontal — white | Transparent vector master |
| [ruxbase-icon-black.png](ruxbase-icon-black.png) | Square icon — black | 1024 px transparent PNG |
| [ruxbase-icon-white.png](ruxbase-icon-white.png) | Square icon — white | 1024 px transparent PNG |
| [favicon-16.png](favicon-16.png) | Favicon 16 px | PNG |
| [favicon-24.png](favicon-24.png) | Favicon 24 px | PNG |
| [favicon-32.png](favicon-32.png) | Favicon 32 px | PNG |
| [favicon-48.png](favicon-48.png) | Favicon 48 px | PNG |
| [ruxbase-construction-grid.svg](ruxbase-construction-grid.svg) | Construction grid | SVG diagram |
| [ruxbase-construction-grid.png](ruxbase-construction-grid.png) | Construction grid | PNG diagram |

The logo assets have transparent backgrounds; vector lockup wordmarks are outlined paths. There are eight SVG logo variants, two 1024 px PNG icons, four favicon images, two construction-grid files and three cross-theme PNGs.

## References and limitations

- [Apple HIG — App icons](https://developer.apple.com/design/human-interface-guidelines/app-icons/)
- [IBM Design Language — 8-Bar](https://www.ibm.com/design/language/ibm-logos/8-bar/)

Only principles of simplicity and clear space were used, not the reference logo shapes. The original reconstruction reported approximately 97.2% area overlap after normalizing dimensions; this value has not been independently remeasured here. Supporting typography, colors and usage limits are proposed. This guide does not include trademark clearance or testing on every platform.

When updating the visual guidelines, keep the Markdown and JSON companions synchronized with the HTML and actual SVG assets. Do not infer new specifications from undocumented values.
