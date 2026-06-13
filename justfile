[private]
default:
    @just --list

rebuild:
  make
  ./modctl completion zsh > ~/.local/share/zsh/completions/_modctl
  cp ./modctl ~/bin
  exec zsh

copy-assets:
  cp node_modules/elasticlunr/release/elasticlunr.min.js www/static
  cp node_modules/elasticlunr/LICENSE www/static/LICENSE-elasticlunr.txt
  cp node_modules/@fontsource-variable/sixtyfour/files/sixtyfour-latin-bled-normal.woff2 www/static
  cp node_modules/@fontsource-variable/sixtyfour/LICENSE www/static/LICENSE-sixtyfour.txt

regenerate-favicon:
  if [ ! -f www/favicon.png ]; then \
    magick -gravity center -fill black -size 512x512 -background none \
        -font "${SIXTYFOUR_PATH}" caption:">M" png:- | \
        magick - -trim +repage -gravity center -background none -extent 512x512 \
        www/favicon.png \
  ;fi

copy-content:
  ./www/syncdocs.bash

generate-favicon-bundle: regenerate-favicon
  if [ ! -f www/static/favicon.ico ]; then \
    pnpm exec realfavicon generate www/favicon.png www/favicon.json \
      www/out.json www/static \
  ;fi

[working-directory: 'www']
zola-build: generate-favicon-bundle copy-assets copy-content
  zola build --minify

[working-directory: 'www']
zola-serve: generate-favicon-bundle copy-assets copy-content
  zola serve
