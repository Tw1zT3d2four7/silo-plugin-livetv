FROM scratch
LABEL org.opencontainers.image.title="Silo Live TV plugin"
LABEL org.opencontainers.image.description="Packaged Linux amd64 Silo plugin executable and manifest"
COPY silo-plugin-livetv /silo-plugin-livetv
COPY manifest.json /manifest.json
COPY silo-plugin-livetv.sha256 /silo-plugin-livetv.sha256
