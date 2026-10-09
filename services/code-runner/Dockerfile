# syntax=docker/dockerfile:1
# Console/web profile. Official amd64 manifest resolved 2026-10-09.
FROM node:24-trixie-slim@sha256:c83040f7e24bacea68e58a0b96a484ba430bf24abb2973eab7ba4701144b7b63
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-venv lua5.4 ruby php-cli r-base-core perl sqlite3 && rm -rf /var/lib/apt/lists/* && python3 -m venv /opt/python
COPY runtime/package.json runtime/package-lock.json /opt/node-libraries/
RUN --mount=type=secret,id=proxy_ca if [ -f /run/secrets/proxy_ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/proxy_ca; fi; npm ci --prefix /opt/node-libraries --ignore-scripts && node /opt/node-libraries/node_modules/esbuild/install.js && chmod -R a+rX /opt/node-libraries
COPY runtime/requirements.txt /opt/python/requirements.txt
RUN --mount=type=secret,id=system_ca if [ -f /run/secrets/system_ca ]; then export PIP_CERT=/run/secrets/system_ca; fi; /opt/python/bin/pip install --require-hashes --only-binary=:all: -r /opt/python/requirements.txt

# Controller has no secrets. Submitted programs are demoted to UID/GID 65532.
RUN find / -xdev -type f -perm /6000 -exec chmod a-s {} + && mkdir -p /opt/edsger
COPY runtime/runner.py /opt/edsger/runner.py
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/bin/python3", "-u", "/opt/edsger/runner.py"]
