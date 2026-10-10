sed := if os() == "macos" { "gsed" } else { "sed" }
today := `date +%Y-%m-%d`
url := "https://github.com/mfinelli/modctl"

[private]
default:
    @just --list

bump v:
    @grep -qi '^## unreleased' CHANGELOG.md
    {{ sed }} -i -E "s|^## unreleased|## v{{ v }} - {{ today }}|" CHANGELOG.md
    {{ sed }} -i -E \
        "s|(LABEL org\.opencontainers\.image\.version=).*|\1{{ v }}|" \
        Dockerfile
    {{ sed }} -i -E "s|(Version:\s+\").*(\",)|\1{{ v }}\2|" cmd/root.go
    {{ sed }} -i -E "s|^(current_version = \"v).*\"|\1{{ v }}\"|" www/zola.toml
    {{ sed }} -i -E "s|^(release_url = \"{{ url }}/releases/tag/v).*\"|\1{{ v }}\"|" \
        www/zola.toml

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

[working-directory('www')]
zola-build: generate-favicon-bundle copy-assets copy-content
    zola build --minify

[working-directory('www')]
zola-serve: generate-favicon-bundle copy-assets copy-content
    zola serve
