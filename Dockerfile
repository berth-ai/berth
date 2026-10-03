# syntax=docker/dockerfile:1.19

# renovate: datasource=docker depName=ubuntu
ARG UBUNTU_IMAGE=ubuntu:24.04@sha256:008173c23f95b170204355c12626cb5a965d779a7e1283b09e9cffbb1bf33ca3
# renovate: datasource=docker depName=golang
ARG GO_IMAGE=golang:1.27.1-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195

# ---------------------------------------------------------------- go-build
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS go-build
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
# Declared after the cached layers: a new commit must not invalidate them.
ARG BERTH_VERSION=dev
ARG BERTH_COMMIT=unknown
ARG BERTH_REPO=berth-ai/berth
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X main.version=${BERTH_VERSION} -X main.commit=${BERTH_COMMIT} -X main.defaultRepo=${BERTH_REPO}" \
      -o /out/berth ./cmd/berth

# ------------------------------------------------------------------- tools
FROM ${UBUNTU_IMAGE} AS tools
ARG TARGETARCH
# renovate: datasource=github-releases depName=jdx/mise extractVersion=^v(?<version>.*)$
ARG MISE_VERSION=2026.9.17
ARG MISE_SHA256_AMD64=63049bc35fb9065e8dc35ac8b25fdae53e9bd6f1885a843aedeba398e046a1ee
ARG MISE_SHA256_ARM64=ab125726a93a2967e30290ade713a960b9fe8eed0ae0e2fc0976e327b08af70d
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git xz-utils bzip2 unzip \
 && rm -rf /var/lib/apt/lists/*
RUN --mount=type=bind,source=build/fetch-artifact.sh,target=/usr/local/bin/fetch-artifact \
    case "${TARGETARCH}" in \
      amd64) arch=x64; sha=${MISE_SHA256_AMD64} ;; \
      arm64) arch=arm64; sha=${MISE_SHA256_ARM64} ;; \
      *) echo "unsupported architecture ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && fetch-artifact "https://github.com/jdx/mise/releases/download/v${MISE_VERSION}/mise-v${MISE_VERSION}-linux-${arch}" "${sha}" /usr/local/bin/mise \
 && chmod 0755 /usr/local/bin/mise
ENV MISE_DATA_DIR=/opt/mise \
    MISE_CACHE_DIR=/tmp/mise-cache \
    MISE_CONFIG_DIR=/tmp/mise-config \
    MISE_STATE_DIR=/tmp/mise-state \
    MISE_YES=1
WORKDIR /tmp/berth-tools
COPY tools/mise.toml ./mise.toml
RUN --mount=type=secret,id=github_token,required=false \
    --mount=type=bind,source=build/mise-wrappers.sh,target=/usr/local/bin/mise-wrappers \
    if [[ -s /run/secrets/github_token ]]; then GITHUB_TOKEN="$(cat /run/secrets/github_token)"; export GITHUB_TOKEN; fi \
 && mise trust ./mise.toml \
 && mise install \
 && mise bin-paths | xargs mise-wrappers /opt/mise/wrappers \
 && rm -rf /tmp/mise-cache /tmp/mise-state /tmp/mise-config

# ----------------------------------------------------------------- runtime
FROM ${UBUNTU_IMAGE} AS runtime
ARG UBUNTU_IMAGE
ARG TARGETARCH
# renovate: datasource=github-releases depName=tsl0922/ttyd
ARG TTYD_VERSION=1.7.7
ARG TTYD_SHA256_AMD64=8a217c968aba172e0dbf3f34447218dc015bc4d5e59bf51db2f2cd12b7be4f55
ARG TTYD_SHA256_ARM64=b38acadd89d1d396a0f5649aa52c539edbad07f4bc7348b27b4f4b7219dd4165
# renovate: datasource=github-releases depName=coder/code-server extractVersion=^v(?<version>.*)$
ARG CODE_SERVER_VERSION=4.139.1
ARG CODE_SERVER_SHA256_AMD64=53029be6c5781b7bca49b815fcc9a2a3fc111813ad8c9965b2c0f0d2985a0674
ARG CODE_SERVER_SHA256_ARM64=0edb4b60d9c4744b2dd14b0911e3c2e6dd8c6f3c13bd58bda23ae744e59e7df1
# renovate: datasource=github-tags depName=tmux-plugins/tmux-resurrect
ARG TMUX_RESURRECT_VERSION=v4.0.0
ARG TMUX_RESURRECT_SHA256=f8559d86d81be769054d63b28a268229a09bc6cc311f0623656c3090ec781b76
# renovate: datasource=github-tags depName=tmux-plugins/tmux-continuum
ARG TMUX_CONTINUUM_VERSION=v3.1.0
ARG TMUX_CONTINUUM_SHA256=311dddeebbf7b803cd19c339ab887784b715fd8ef206ec430028cabb8574de87

SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
ENV DEBIAN_FRONTEND=noninteractive TZ=UTC

# Core packages. On a failed install, read the first E:/Err: line, not the exit code.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      passwd tini gosu ca-certificates curl git zsh tmux locales tzdata \
      openssh-client jq less procps xz-utils \
 && sed -i 's/^# *en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && locale-gen \
 && rm -rf /var/lib/apt/lists/*

# Runtime libraries for Playwright's headless Chromium.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      libasound2t64 libatk-bridge2.0-0t64 libatk1.0-0t64 libatspi2.0-0t64 libcairo2 \
      libcups2t64 libdbus-1-3 libdrm2 libgbm1 libglib2.0-0t64 libnspr4 libnss3 \
      libpango-1.0-0 libx11-6 libxcb1 libxcomposite1 libxdamage1 libxext6 libxfixes3 \
      libxkbcommon0 libxrandr2 fonts-liberation fonts-noto-color-emoji \
 && rm -rf /var/lib/apt/lists/*

ENV LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8

# Frontends and tmux plugins, verified by sha256.
RUN --mount=type=bind,source=build/fetch-artifact.sh,target=/usr/local/bin/fetch-artifact \
    case "${TARGETARCH}" in \
      amd64) ttyd_arch=x86_64; ttyd_sha=${TTYD_SHA256_AMD64}; cs_sha=${CODE_SERVER_SHA256_AMD64} ;; \
      arm64) ttyd_arch=aarch64; ttyd_sha=${TTYD_SHA256_ARM64}; cs_sha=${CODE_SERVER_SHA256_ARM64} ;; \
      *) echo "unsupported architecture ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
 && fetch-artifact "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.${ttyd_arch}" "${ttyd_sha}" /usr/local/bin/ttyd \
 && chmod 0755 /usr/local/bin/ttyd \
 && fetch-artifact "https://github.com/coder/code-server/releases/download/v${CODE_SERVER_VERSION}/code-server-${CODE_SERVER_VERSION}-linux-${TARGETARCH}.tar.gz" "${cs_sha}" /tmp/code-server.tgz \
 && mkdir -p /usr/lib/code-server \
 && tar -xzf /tmp/code-server.tgz -C /usr/lib/code-server --strip-components=1 \
 && rm /tmp/code-server.tgz \
 && printf '#!/bin/sh\nexec /usr/lib/code-server/bin/code-server "$@"\n' >/usr/local/bin/code-server \
 && chmod 0755 /usr/local/bin/code-server \
 && for plugin in "tmux-resurrect ${TMUX_RESURRECT_VERSION} ${TMUX_RESURRECT_SHA256}" \
                  "tmux-continuum ${TMUX_CONTINUUM_VERSION} ${TMUX_CONTINUUM_SHA256}"; do \
      read -r name version sha <<<"${plugin}"; \
      fetch-artifact "https://codeload.github.com/tmux-plugins/${name}/tar.gz/refs/tags/${version}" "${sha}" "/tmp/${name}.tgz"; \
      mkdir -p "/usr/share/berth/tmux-plugins/${name}"; \
      tar -xzf "/tmp/${name}.tgz" -C "/usr/share/berth/tmux-plugins/${name}" --strip-components=1; \
      rm "/tmp/${name}.tgz"; \
    done

COPY --from=tools /usr/local/bin/mise /usr/local/bin/mise
COPY --from=tools /opt/mise /opt/mise
COPY --from=go-build /out/berth /usr/local/bin/berth
COPY rootfs/ /
COPY agents/package.json agents/package-lock.json /usr/share/berth/agents/
COPY tools/mise.toml /usr/share/berth/mise.toml
COPY tools/presets/ /usr/share/berth/presets/

# The agent user replaces Ubuntu's default uid-1000 user. berth-panel runs the
# web panel and alone shares the broker socket's group. /workspaces is created
# here and never chowned at runtime.
RUN userdel --remove ubuntu 2>/dev/null || true \
 && groupadd --gid 1000 agent \
 && useradd --uid 1000 --gid 1000 --create-home --home-dir /home/agent --shell /usr/bin/zsh agent \
 && groupadd --system berth-panel \
 && useradd --system --gid berth-panel --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin berth-panel \
 && install -d -o 1000 -g 1000 /workspaces \
 && install -d -m 0755 /opt/agents /run/berth \
 && install -d -m 0755 /var/lib/berth \
 && printf '\n# Berth\nsource /etc/berth/zshenv\n' >>/etc/zsh/zshenv \
 && printf '\n# Berth\nsource /etc/berth/zshrc\n' >>/etc/zsh/zshrc

# Per-commit values, declared late so a new commit reuses every layer above.
ARG BERTH_VERSION=dev
ARG BERTH_COMMIT=unknown
ARG BERTH_REPO=berth-ai/berth
RUN --mount=type=bind,source=build/write-build-info.sh,target=/usr/local/bin/write-build-info \
    write-build-info /usr/share/berth/build-info.json "${BERTH_VERSION}" "${BERTH_COMMIT}" \
      "${TARGETARCH}" "${UBUNTU_IMAGE#*@}" "${TTYD_VERSION}" "${CODE_SERVER_VERSION}" \
      /usr/share/berth/agents/package-lock.json

ENV BERTH_REPO=${BERTH_REPO} \
    DISABLE_AUTOUPDATER=1 \
    EDITOR=nvim \
    VISUAL=nvim \
    XDG_CONFIG_DIRS=/etc/berth/defaults/xdg:/etc/xdg \
    PATH=/var/lib/berth/bin:/opt/mise/wrappers:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

EXPOSE 7690
HEALTHCHECK --interval=30s --timeout=10s --start-period=600s --start-interval=2s --retries=3 \
  CMD ["berth-healthcheck"]
ENTRYPOINT ["tini", "--", "berth-entrypoint"]
CMD ["serve"]
