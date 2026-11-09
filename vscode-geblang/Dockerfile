FROM node:20-slim AS build

WORKDIR /ext

# Install dependencies first (cache layer)
COPY package.json ./
RUN npm install

# Copy source and compile/bundle
COPY tsconfig.json build.mjs ./
COPY src/ ./src/
COPY syntaxes/ ./syntaxes/
COPY snippets/ ./snippets/
COPY language-configuration.json gebweb-template-configuration.json README.md CHANGELOG.md LICENSE .vscodeignore ./
COPY images/ ./images/
RUN npm run typecheck
RUN npm run compile

# Package as VSIX; no node_modules needed since everything is bundled
RUN npx --yes @vscode/vsce package --no-dependencies --out /ext/geblang.vsix

# Export compiled output and VSIX as a minimal artifacts image
FROM scratch AS artifacts
COPY --from=build /ext/out /out/
COPY --from=build /ext/geblang.vsix /vsix/geblang.vsix
CMD ["/vsix/geblang.vsix"]
