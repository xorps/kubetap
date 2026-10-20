FROM mitmproxy/mitmproxy:12.2.3@sha256:00b77b5d8804c8ad18cb6caefbf9d5849e895e8986c5ce011f4ae30f4385962f

# The upstream image is Debian-based; pick up security fixes released since it was built.
RUN apt-get update && \
    apt-get upgrade -y && \
    rm -rf /var/lib/apt/lists/*

# Unbuffered output so the mitmweb token URL shows up in `kubectl logs`.
ENV PYTHONUNBUFFERED=1

# HACK: the security context of the injected pod could be run as any user, therefore
# all users must be able to write to the directory.
RUN chmod -R 777 /home/mitmproxy/.mitmproxy/

# Hijack the mitmproxy entrypoint (docker-entrypoint.sh) so that
# configuration can be built from within the container using the
# kubetap binary.

RUN --mount=type=bind,source=kubetap-entrypoint.sh,target=/tmp/kubetap-entrypoint.sh \
    install -m 0755 /tmp/kubetap-entrypoint.sh /usr/local/bin/kubetap-entrypoint.sh
ENTRYPOINT ["kubetap-entrypoint.sh"]
