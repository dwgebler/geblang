# Changelog

## 1.7.0

- Geblang 1.42.0 support, including image drawing, text, fonts and barcode
  completion, hover and signature help through the installed language server.
- `imagedraw` snippet for creating a canvas, drawing shapes and text, and saving
  the image; `barcodeqr` snippet for generating and saving a QR code.
- `jobs-worker` snippet for a durable jobs worker entry point.
- Use Geblang 1.42.0 or later for these standard library APIs.

## 1.6.0

- Extension icon, matching the Geblang website.
- Marketplace README covering features, requirements, commands and settings.
- `securerandomints` snippet for `secureRandom.randomInts` (Geblang 1.41.1).
- Gebweb support: a Gebweb Template language for `.html` views in Gebweb
  projects, Gebweb and template snippets, and Gebweb CLI commands (new
  project, dev server, routes, generate, migrate, build, worker).
- The packaged extension now includes the README, changelog and license, and
  no longer ships TypeScript sources or source maps.
- Requires Geblang 1.41 or later for workspace-aware import resolution.

Earlier versions are described in the Geblang release notes.
