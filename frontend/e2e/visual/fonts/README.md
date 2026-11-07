# Visual-test font environment

The visual Chromium project uses the unmodified Liberation Fonts 2.x TTF files from Debian's `fonts-liberation` package (`/usr/share/fonts/truetype/liberation`, installed package version 1:2.1.5-3 on the capture host). Source: https://github.com/liberationfonts/liberation-fonts. License: SIL Open Font License 1.1, text included as `OFL-LICENSE.txt` from `/usr/share/doc/fonts-liberation/copyright`.

`fonts.conf` reads only this sibling directory and aliases system-ui/sans-serif, serif and monospace to the corresponding Liberation families. `FONTCONFIG_FILE` is scoped to the visual Playwright child process by the native runner, so application CSS and normal browser tests are untouched. The config uses a relative font directory, and can therefore travel with the repository. Chromium's default fontconfig fallback is replaced for this process.
