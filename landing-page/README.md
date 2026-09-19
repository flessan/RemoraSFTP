# RemoraSFTP — landing page

Static marketing site for RemoraSFTP. Plain HTML/CSS/JS — no React, no Vite,
no build step, no Go code, and no coupling to the application frontend in
[`/web`](../web). The Three.js hero is loaded from a CDN via dynamic import
and degrades to a static SVG scene when WebGL or the CDN is unavailable.

## Files

```
landing-page/
├── index.html      single page
├── styles.css      all styling
├── script.js       nav, scroll reveal, Three.js hero
└── assets/
    └── favicon.svg
```

## Deploying to Cloudflare Pages

This directory is designed to be the Pages project root:

| Setting          | Value                          |
| ---------------- | ------------------------------ |
| Build command    | *(none)*                       |
| Output directory | `landing-page` (repo subpath)  |

Or connect the repo with the output directory set to `landing-page` and no
build command. No server-side runtime or framework adapter is required.

https://remora-sftp.pages.dev
