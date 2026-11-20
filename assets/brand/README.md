# Ruxbase brand assets

The existing PNG originals remain at their current paths and are the visual
reference for Ruxbase. See [manifest.json](manifest.json) for their exact Git
blob IDs and dimensions.

- ruxbase.png: full horizontal lockup.
- ruxbase_logo.png: square R symbol for compact applications.
- ruxbase_name.png: standalone uppercase wordmark.

Do not redraw or recolor the identity without review. Keep the original geometry
and spacing. Use dark marks on light surfaces and light marks on dark surfaces
after tested vector derivatives are approved. Preserve the outer silhouette
and negative space when optimizing for small sizes.

`tokens.css` specifies the initial documentation palette. It is a design
proposal, not a color extraction from the PNGs or a verified contrast audit.
Production exports will live in svg/, icons/ and social/ once visually approved.
Do not label raster wrappers as vector logos. Keep the original paths for existing users.

## Vector candidates

`svg-candidates/` contains manually traced two-path geometry based on the original square symbol, with light/dark surface variants and favicon. These are genuine vector candidates, not certified exact reproductions. Original PNGs remain authoritative. The favicon is wired to the development build; the header retains the original PNG until visual review. Wordmark and full lockup vectorization, raster derivatives and small-size visual review remain open.
