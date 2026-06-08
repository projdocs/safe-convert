# ── Stage 1: installer ────────────────────────────────────────────
FROM debian:12-slim AS installer

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        libreoffice-writer \
        libreoffice-calc \
        libreoffice-impress \
        libreoffice-draw \
        fonts-liberation \
        fonts-liberation2 \
        fonts-noto-core \
        fonts-crosextra-carlito \
        fonts-crosextra-caladea \
        libxinerama1 \
        libxrandr2 \
        libxrender1 \
        fontconfig \
    && apt-get clean \
    && rm -rf \
        /var/lib/apt/lists/* \
        /var/cache/apt/archives/* \
        /tmp/* \
        /var/tmp/*

# Build the font cache inside the installer stage where fc-cache is available.
# The resulting cache directory is copied into the runtime stage below.
RUN fc-cache -fv

# ── Stage 2: hardened runtime ─────────────────────────────────────
FROM debian:12-slim AS runtime

COPY --from=installer /usr/lib/libreoffice        /usr/lib/libreoffice
COPY --from=installer /usr/bin/libreoffice        /usr/bin/libreoffice
COPY --from=installer /usr/share/fonts            /usr/share/fonts
COPY --from=installer /usr/share/libreoffice      /usr/share/libreoffice
COPY --from=installer /etc/fonts                  /etc/fonts
COPY --from=installer /usr/lib/x86_64-linux-gnu   /usr/lib/x86_64-linux-gnu
COPY --from=installer /lib/x86_64-linux-gnu       /lib/x86_64-linux-gnu

# Copy the pre-built font cache — no tooling required in the runtime stage.
COPY --from=installer /var/cache/fontconfig       /var/cache/fontconfig
COPY --from=installer /root/.cache/fontconfig     /root/.cache/fontconfig

RUN groupadd --gid 10001 svcgroup && \
    useradd \
      --uid 10001 \
      --gid 10001 \
      --no-create-home \
      --shell /usr/sbin/nologin \
      --comment "LibreOffice service account" \
      svcuser

# Remove setuid/setgid bits from every binary in the image.
RUN find / -xdev \( -perm -4000 -o -perm -2000 \) -exec chmod ug-s {} + 2>/dev/null || true

# Remove any shells pulled in transitively.
RUN rm -f \
    /bin/sh \
    /bin/bash \
    /bin/dash \
    /usr/bin/sh \
    /usr/bin/bash \
    /usr/bin/perl \
    /usr/bin/python3 \
    2>/dev/null || true

USER svcuser