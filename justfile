[private]
default:
    @just --list

copy-assets:
  cp node_modules/elasticlunr/release/elasticlunr.min.js www/static
  cp node_modules/elasticlunr/LICENSE www/static/LICENSE-elasticlunr.txt
  cp node_modules/@fontsource-variable/sixtyfour/files/sixtyfour-latin-bled-normal.woff2 www/static
  cp node_modules/@fontsource-variable/sixtyfour/LICENSE www/static/LICENSE-sixtyfour.txt

convert-woff-to-ttf:
  fontforge -lang=ff -c 'Open($1); Generate($2); Close();' \
    node_modules/@fontsource-variable/sixtyfour/files/sixtyfour-latin-bled-normal.woff2 \
    www/sixtyfour.ttf

regenerate-favicon: convert-woff-to-ttf
  magick -gravity center -fill black -size 512x512 -background none \
      -font www/sixtyfour.ttf caption:">m" png:- | \
      magick - -trim +repage -gravity center -background none -extent 512x512 \
      www/favicon.png

copy-content:
  ./www/syncdocs.bash

[working-directory: 'www']
zola-build: regenerate-favicon copy-assets copy-content
  zola build --minify

[working-directory: 'www']
zola-serve: regenerate-favicon copy-assets copy-content
  zola serve
