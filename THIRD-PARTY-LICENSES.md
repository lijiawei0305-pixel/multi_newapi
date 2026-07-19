# Third-Party Licenses

This file inventories direct third-party dependencies used to build or distribute this project.
It is an engineering compliance artifact and should be kept with Docker images, standalone binaries, frontend bundles, and Electron installers.

Inventory snapshot: 2026-07-19.

Scope and provenance:

- Go: every non-indirect requirement in `go.mod`; versions come from the manifest and license identifiers are classified from local module-cache license, licence, or copying files.
- Web: production and development dependencies from `web/default/package.json` and `web/classic/package.json`; resolved versions come from `web/bun.lock` and licenses from the corresponding installed package metadata.
- Electron: development dependencies from `electron/package.json`; resolved versions and license metadata are cross-checked against `electron/package-lock.json`.
- Orbit landing page: production and development dependencies from `bulb-orbit/v2/package.json`; resolved versions and license metadata are cross-checked against `bulb-orbit/v2/package-lock.json`.
- `web/package.json` currently declares no direct dependencies. Its catalog and overrides are resolution policy, not additional direct dependencies.
- Transitive dependencies are outside this inventory and still require a release-specific audit.

The inventory contains exactly 207 direct dependencies: 63 Go modules and 144 npm packages.

## Dependency Inventory

| Area | Scope | Ecosystem | Dependency | Resolved version | License |
|---|---|---|---|---|---|
| backend | production | Go | `github.com/abema/go-mp4` | `v1.4.1` | MIT |
| backend | test | Go | `github.com/alicebob/miniredis/v2` | `v2.38.0` | MIT |
| backend | production | Go | `github.com/andybalholm/brotli` | `v1.1.1` | MIT |
| backend | production | Go | `github.com/anknown/ahocorasick` | `v0.0.0-20190904063843-d75dbd5169c0` | MIT |
| backend | production | Go | `github.com/aws/aws-sdk-go-v2` | `v1.41.5` | Apache-2.0 |
| backend | production | Go | `github.com/aws/aws-sdk-go-v2/credentials` | `v1.19.10` | Apache-2.0 |
| backend | production | Go | `github.com/aws/aws-sdk-go-v2/service/bedrockruntime` | `v1.50.4` | Apache-2.0 |
| backend | production | Go | `github.com/aws/smithy-go` | `v1.24.2` | Apache-2.0 |
| backend | production | Go | `github.com/bytedance/gopkg` | `v0.1.3` | Apache-2.0 |
| backend | production | Go | `github.com/casbin/casbin/v2` | `v2.135.0` | Apache-2.0 |
| backend | production | Go | `github.com/gin-contrib/cors` | `v1.7.2` | MIT |
| backend | production | Go | `github.com/gin-contrib/gzip` | `v0.0.6` | MIT |
| backend | production | Go | `github.com/gin-contrib/sessions` | `v0.0.5` | MIT |
| backend | production | Go | `github.com/gin-contrib/static` | `v0.0.1` | MIT |
| backend | production | Go | `github.com/gin-gonic/gin` | `v1.9.1` | MIT |
| backend | production | Go | `github.com/glebarez/sqlite` | `v1.9.0` | MIT |
| backend | production | Go | `github.com/go-audio/aiff` | `v1.1.0` | Apache-2.0 |
| backend | production | Go | `github.com/go-audio/wav` | `v1.1.0` | Apache-2.0 |
| backend | production | Go | `github.com/go-playground/validator/v10` | `v10.20.0` | MIT |
| backend | production | Go | `github.com/go-redis/redis/v8` | `v8.11.5` | BSD-2-Clause |
| backend | production | Go | `github.com/go-webauthn/webauthn` | `v0.14.0` | BSD-3-Clause |
| backend | production | Go | `github.com/golang-jwt/jwt/v5` | `v5.3.0` | MIT |
| backend | production | Go | `github.com/google/uuid` | `v1.6.0` | BSD-3-Clause |
| backend | production | Go | `github.com/gorilla/websocket` | `v1.5.0` | BSD-2-Clause |
| backend | production | Go | `github.com/grafana/pyroscope-go` | `v1.2.7` | Apache-2.0 |
| backend | production | Go | `github.com/jfreymuth/oggvorbis` | `v1.0.5` | MIT |
| backend | production | Go | `github.com/jinzhu/copier` | `v0.4.0` | MIT |
| backend | production | Go | `github.com/joho/godotenv` | `v1.5.1` | MIT |
| backend | production | Go | `github.com/mewkiz/flac` | `v1.0.13` | Unlicense |
| backend | production | Go | `github.com/nicksnyder/go-i18n/v2` | `v2.6.1` | MIT |
| backend | production | Go | `github.com/pkg/errors` | `v0.9.1` | BSD-2-Clause |
| backend | production | Go | `github.com/pquerna/otp` | `v1.5.0` | Apache-2.0 |
| backend | production | Go | `github.com/samber/hot` | `v0.11.0` | MIT |
| backend | production | Go | `github.com/samber/lo` | `v1.52.0` | MIT |
| backend | production | Go | `github.com/shirou/gopsutil` | `v3.21.11+incompatible` | BSD-3-Clause |
| backend | production | Go | `github.com/shopspring/decimal` | `v1.4.0` | MIT |
| backend | production | Go | `github.com/smartwalle/alipay/v3` | `v3.2.29` | MIT |
| backend | production | Go | `github.com/stretchr/testify` | `v1.11.1` | MIT |
| backend | production | Go | `github.com/stripe/stripe-go/v81` | `v81.4.0` | MIT |
| backend | production | Go | `github.com/tcolgate/mp3` | `v0.0.0-20170426193717-e79c5a46d300` | MIT |
| backend | production | Go | `github.com/thanhpk/randstr` | `v1.0.6` | MIT |
| backend | production | Go | `github.com/tidwall/gjson` | `v1.18.0` | MIT |
| backend | production | Go | `github.com/tidwall/sjson` | `v1.2.5` | MIT |
| backend | production | Go | `github.com/tiktoken-go/tokenizer` | `v0.6.2` | MIT |
| backend | production | Go | `github.com/waffo-com/waffo-go` | `v1.3.2` | MIT |
| backend | production | Go | `github.com/wechatpay-apiv3/wechatpay-go` | `v0.2.21` | Apache-2.0 |
| backend | production | Go | `golang.org/x/crypto` | `v0.48.0` | BSD-3-Clause |
| backend | production | Go | `golang.org/x/image` | `v0.38.0` | BSD-3-Clause |
| backend | production | Go | `golang.org/x/net` | `v0.50.0` | BSD-3-Clause |
| backend | production | Go | `golang.org/x/sync` | `v0.20.0` | BSD-3-Clause |
| backend | production | Go | `golang.org/x/sys` | `v0.41.0` | BSD-3-Clause |
| backend | production | Go | `golang.org/x/text` | `v0.35.0` | BSD-3-Clause |
| backend | production | Go | `gopkg.in/yaml.v3` | `v3.0.1` | MIT AND Apache-2.0 |
| backend | production | Go | `gorm.io/driver/mysql` | `v1.4.3` | MIT |
| backend | production | Go | `gorm.io/driver/postgres` | `v1.5.2` | MIT |
| backend | production | Go | `gorm.io/gorm` | `v1.25.2` | MIT |
| backend | production | Go | `github.com/waffo-com/waffo-pancake-sdk-go` | `v0.3.1` | MIT |
| backend | production | Go | `gorm.io/driver/clickhouse` | `v0.6.0` | MIT |
| backend | production | Go | `github.com/Azure/go-ntlmssp` | `v0.1.1` | MIT |
| backend | production | Go | `github.com/ClickHouse/clickhouse-go/v2` | `v2.32.0` | Apache-2.0 |
| backend | production | Go | `github.com/expr-lang/expr` | `v1.17.8` | MIT |
| backend | production | Go | `github.com/go-sql-driver/mysql` | `v1.7.0` | MPL-2.0 |
| backend | production | Go | `github.com/klauspost/compress` | `v1.18.0` | BSD-3-Clause AND Apache-2.0 |
| web/default | production | npm | `@base-ui/react` | `1.5.0` | MIT |
| web/default | production | npm | `@codemirror/lang-markdown` | `6.5.0` | MIT |
| web/default | production | npm | `@codemirror/language` | `6.12.4` | MIT |
| web/default | production | npm | `@codemirror/state` | `6.7.0` | MIT |
| web/default | production | npm | `@codemirror/view` | `6.43.3` | MIT |
| web/default | production | npm | `@fontsource-variable/lora` | `5.2.8` | OFL-1.1 |
| web/default | production | npm | `@fontsource-variable/public-sans` | `5.2.7` | OFL-1.1 |
| web/default | production | npm | `@hookform/resolvers` | `5.4.0` | MIT |
| web/default | production | npm | `@hugeicons/core-free-icons` | `4.2.0` | MIT |
| web/default | production | npm | `@hugeicons/react` | `1.1.6` | MIT |
| web/default | production | npm | `@lezer/highlight` | `1.2.3` | MIT |
| web/default | production | npm | `@lobehub/icons` | `5.10.0` | MIT |
| web/default | production | npm | `@tailwindcss/postcss` | `4.3.0` | MIT |
| web/default | production | npm | `@tanstack/react-query` | `5.100.14` | MIT |
| web/default | production | npm | `@tanstack/react-router` | `1.170.10` | MIT |
| web/default | production | npm | `@tanstack/react-table` | `8.21.3` | MIT |
| web/default | production | npm | `@visactor/react-vchart` | `2.0.22` | MIT |
| web/default | production | npm | `@visactor/vchart` | `2.0.22` | MIT |
| web/default | production | npm | `ai` | `6.0.193` | Apache-2.0 |
| web/default | production | npm | `auto-skeleton-react` | `1.0.5` | MIT |
| web/default | production | npm | `axios` | `1.16.1` | MIT |
| web/default | production | npm | `class-variance-authority` | `0.7.1` | Apache-2.0 |
| web/default | production | npm | `clsx` | `2.1.1` | MIT |
| web/default | production | npm | `cmdk` | `1.1.1` | MIT |
| web/default | production | npm | `date-fns` | `4.4.0` | MIT |
| web/default | production | npm | `dayjs` | `1.11.21` | MIT |
| web/default | production | npm | `dompurify` | `3.4.11` | (MPL-2.0 OR Apache-2.0) |
| web/default | production | npm | `i18next` | `26.3.0` | MIT |
| web/default | production | npm | `i18next-browser-languagedetector` | `8.2.1` | MIT |
| web/default | production | npm | `input-otp` | `1.4.2` | MIT |
| web/default | production | npm | `katex` | `0.17.0` | MIT |
| web/default | production | npm | `lucide-react` | `1.17.0` | ISC |
| web/default | production | npm | `marked` | `18.0.5` | MIT |
| web/default | production | npm | `motion` | `12.40.0` | MIT |
| web/default | production | npm | `nanoid` | `5.1.11` | MIT |
| web/default | production | npm | `qrcode.react` | `4.2.0` | ISC |
| web/default | production | npm | `react` | `19.2.6` | MIT |
| web/default | production | npm | `react-day-picker` | `10.0.1` | MIT |
| web/default | production | npm | `react-dom` | `19.2.6` | MIT |
| web/default | production | npm | `react-hook-form` | `7.77.0` | MIT |
| web/default | production | npm | `react-i18next` | `17.0.8` | MIT |
| web/default | production | npm | `react-icons` | `5.6.0` | MIT |
| web/default | production | npm | `react-resizable-panels` | `4.11.2` | MIT |
| web/default | production | npm | `react-top-loading-bar` | `3.0.2` | MIT |
| web/default | production | npm | `recharts` | `3.8.1` | MIT |
| web/default | production | npm | `shiki` | `4.1.0` | MIT |
| web/default | production | npm | `sonner` | `2.0.7` | MIT |
| web/default | production | npm | `sse.js` | `2.8.0` | Apache-2.0 |
| web/default | production | npm | `stream-markdown-parser` | `1.0.7` | MIT |
| web/default | production | npm | `tailwind-merge` | `3.6.0` | MIT |
| web/default | production | npm | `tailwindcss` | `4.3.0` | MIT |
| web/default | production | npm | `three` | `0.185.1` | MIT |
| web/default | production | npm | `tokenlens` | `1.3.1` | MIT |
| web/default | production | npm | `tw-animate-css` | `1.4.0` | MIT |
| web/default | production | npm | `use-stick-to-bottom` | `1.1.4` | MIT |
| web/default | production | npm | `vaul` | `1.1.2` | MIT |
| web/default | production | npm | `zod` | `4.4.3` | MIT |
| web/default | production | npm | `zustand` | `5.0.14` | MIT |
| web/default | development | npm | `@rsbuild/core` | `2.0.9` | MIT |
| web/default | development | npm | `@rsbuild/plugin-react` | `2.0.1` | MIT |
| web/default | development | npm | `@tanstack/react-query-devtools` | `5.100.14` | MIT |
| web/default | development | npm | `@tanstack/react-router-devtools` | `1.167.0` | MIT |
| web/default | development | npm | `@tanstack/router-plugin` | `1.168.13` | MIT |
| web/default | development | npm | `@types/node` | `25.9.1` | MIT |
| web/default | development | npm | `@types/react` | `19.2.15` | MIT |
| web/default | development | npm | `@types/react-dom` | `19.2.3` | MIT |
| web/default | development | npm | `@types/three` | `0.185.1` | MIT |
| web/default | development | npm | `@typescript/native-preview` | `7.0.0-dev.20260621.1` | Apache-2.0 |
| web/default | development | npm | `@xyflow/react` | `12.10.2` | MIT |
| web/default | development | npm | `embla-carousel-react` | `8.6.0` | MIT |
| web/default | development | npm | `knip` | `6.15.0` | ISC |
| web/default | development | npm | `oxfmt` | `0.54.0` | MIT |
| web/default | development | npm | `oxlint` | `1.70.0` | MIT |
| web/default | development | npm | `shadcn` | `4.9.0` | MIT |
| web/default | development | npm | `vitest` | `4.1.10` | MIT |
| web/classic | production | npm | `@douyinfe/semi-icons` | `2.99.3` | MIT |
| web/classic | production | npm | `@douyinfe/semi-illustrations` | `2.99.3` | MIT |
| web/classic | production | npm | `@douyinfe/semi-ui` | `2.99.3` | MIT |
| web/classic | production | npm | `@lobehub/icons` | `5.10.0` | MIT |
| web/classic | production | npm | `@visactor/react-vchart` | `1.8.11` | MIT |
| web/classic | production | npm | `@visactor/vchart` | `1.8.11` | MIT |
| web/classic | production | npm | `@visactor/vchart-semi-theme` | `1.8.8` | MIT |
| web/classic | production | npm | `axios` | `1.16.1` | MIT |
| web/classic | production | npm | `clsx` | `2.1.1` | MIT |
| web/classic | production | npm | `date-fns` | `2.30.0` | MIT |
| web/classic | production | npm | `date-fns-tz` | `1.3.8` | MIT |
| web/classic | production | npm | `dayjs` | `1.11.21` | MIT |
| web/classic | production | npm | `dompurify` | `3.4.11` | (MPL-2.0 OR Apache-2.0) |
| web/classic | production | npm | `highlight.js` | `11.11.1` | BSD-3-Clause |
| web/classic | production | npm | `history` | `5.3.0` | MIT |
| web/classic | production | npm | `i18next` | `23.16.8` | MIT |
| web/classic | production | npm | `i18next-browser-languagedetector` | `7.2.2` | MIT |
| web/classic | production | npm | `katex` | `0.16.47` | MIT |
| web/classic | production | npm | `lucide-react` | `0.511.0` | ISC |
| web/classic | production | npm | `marked` | `4.3.0` | MIT |
| web/classic | production | npm | `mermaid` | `11.15.0` | MIT |
| web/classic | production | npm | `qrcode.react` | `4.2.0` | ISC |
| web/classic | production | npm | `react` | `19.2.6` | MIT |
| web/classic | production | npm | `react-dom` | `19.2.6` | MIT |
| web/classic | production | npm | `react-dropzone` | `14.4.1` | MIT |
| web/classic | production | npm | `react-fireworks` | `1.0.4` | ISC |
| web/classic | production | npm | `react-i18next` | `13.5.0` | MIT |
| web/classic | production | npm | `react-icons` | `5.6.0` | MIT |
| web/classic | production | npm | `react-markdown` | `10.1.0` | MIT |
| web/classic | production | npm | `react-router-dom` | `6.30.4` | MIT |
| web/classic | production | npm | `react-telegram-login` | `1.1.2` | MIT |
| web/classic | production | npm | `react-toastify` | `9.1.3` | MIT |
| web/classic | production | npm | `react-turnstile` | `1.1.5` | MIT |
| web/classic | production | npm | `rehype-highlight` | `7.0.2` | MIT |
| web/classic | production | npm | `rehype-katex` | `7.0.1` | MIT |
| web/classic | production | npm | `remark-breaks` | `4.0.0` | MIT |
| web/classic | production | npm | `remark-gfm` | `4.0.1` | MIT |
| web/classic | production | npm | `remark-math` | `6.0.0` | MIT |
| web/classic | production | npm | `sse.js` | `2.8.0` | Apache-2.0 |
| web/classic | production | npm | `unist-util-visit` | `5.1.0` | MIT |
| web/classic | production | npm | `use-debounce` | `10.1.1` | MIT |
| web/classic | development | npm | `@rsbuild/core` | `2.0.9` | MIT |
| web/classic | development | npm | `@rsbuild/plugin-react` | `2.0.1` | MIT |
| web/classic | development | npm | `@so1ve/prettier-config` | `3.26.0` | MIT |
| web/classic | development | npm | `autoprefixer` | `10.5.0` | MIT |
| web/classic | development | npm | `eslint` | `8.57.0` | MIT |
| web/classic | development | npm | `eslint-plugin-header` | `3.1.1` | MIT |
| web/classic | development | npm | `eslint-plugin-react-hooks` | `5.2.0` | MIT |
| web/classic | development | npm | `i18next-cli` | `1.58.1` | MIT |
| web/classic | development | npm | `jsdom` | `26.1.0` | MIT |
| web/classic | development | npm | `postcss` | `8.5.15` | MIT |
| web/classic | development | npm | `prettier` | `3.8.3` | MIT |
| web/classic | development | npm | `prop-types` | `15.8.1` | MIT |
| web/classic | development | npm | `tailwindcss` | `3.4.19` | MIT |
| web/classic | development | npm | `typescript` | `4.4.2` | Apache-2.0 |
| electron | development | npm | `7zip-bin` | `5.2.0` | MIT |
| electron | development | npm | `cross-env` | `7.0.3` | MIT |
| electron | development | npm | `electron` | `39.8.5` | MIT |
| electron | development | npm | `electron-builder` | `26.15.3` | MIT |
| bulb-orbit/v2 | production | npm | `three` | `0.160.0` | MIT |
| bulb-orbit/v2 | development | npm | `@playwright/test` | `1.61.1` | Apache-2.0 |
| bulb-orbit/v2 | development | npm | `@types/three` | `0.160.0` | MIT |
| bulb-orbit/v2 | development | npm | `eslint` | `9.39.5` | MIT |
| bulb-orbit/v2 | development | npm | `happy-dom` | `20.10.6` | MIT |
| bulb-orbit/v2 | development | npm | `sharp` | `0.35.3` | Apache-2.0 |
| bulb-orbit/v2 | development | npm | `typescript` | `5.9.3` | Apache-2.0 |
| bulb-orbit/v2 | development | npm | `typescript-eslint` | `8.63.0` | MIT |
| bulb-orbit/v2 | development | npm | `vite` | `8.1.5` | MIT |
| bulb-orbit/v2 | development | npm | `vitest` | `4.1.10` | MIT |

## License References

The upstream license and notice files shipped with each dependency are authoritative. The normalized excerpts and references below are a convenience and are not a substitute for preserving dependency-specific copyright, attribution, NOTICE, or reserved-font-name requirements in distributed artifacts.

### Apache-2.0

Apache License
Version 2.0, January 2004
https://www.apache.org/licenses/

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this file except in compliance with the License. You may obtain a copy of
the License at:

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
License for the specific language governing permissions and limitations under
the License.

### MIT AND Apache-2.0

Components with this identifier contain different files under MIT and Apache-2.0. Both sets of terms apply to the distributed component; this is not an either/or election. Both standard license excerpts are included below.

Apache License
Version 2.0, January 2004
https://www.apache.org/licenses/

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this file except in compliance with the License. You may obtain a copy of
the License at:

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
License for the specific language governing permissions and limitations under
the License.

MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

### BSD-3-Clause AND Apache-2.0

Components with this identifier contain separately licensed material under BSD-3-Clause and Apache-2.0. Preserve both upstream license notices. The corresponding normalized excerpts appear in their individual sections below.

### (MPL-2.0 OR Apache-2.0)

The dependency metadata permits distribution under either MPL-2.0 or Apache-2.0. Preserve the upstream license file and document which option is used for a release.

### MPL-2.0

Mozilla Public License 2.0 is a file-level copyleft license. Preserve the complete upstream license file and comply with source-availability requirements for covered-file modifications. The official text is available at https://www.mozilla.org/MPL/2.0/.

### BSD-2-Clause

BSD 2-Clause License

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

### BSD-3-Clause

BSD 3-Clause License

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

### ISC

ISC License

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.

### MIT

MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

### OFL-1.1

SIL Open Font License 1.1

The font dependencies listed under OFL-1.1 are licensed under the SIL Open Font
License, Version 1.1. The full license text is available at:
https://openfontlicense.org/open-font-license-official-text/

When distributing font files, preserve the OFL license text, copyright notices,
and reserved font name restrictions supplied by the upstream font project.

### Unlicense

The Unlicense

This is free and unencumbered software released into the public domain.
Anyone is free to copy, modify, publish, use, compile, sell, or distribute
this software, either in source code form or as a compiled binary, for any
purpose, commercial or non-commercial, and by any means.

For more information, please refer to https://unlicense.org/
