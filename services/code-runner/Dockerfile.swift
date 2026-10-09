# syntax=docker/dockerfile:1
FROM swift:6.2-noble@sha256:1129f7d0490dc1d55a39aabe386f126821b1a5c70c5618b920f9138fd16c257d
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/*

# Controller has no secrets. Submitted programs are demoted to UID/GID 65532.
RUN find / -xdev -type f -perm /6000 -exec chmod a-s {} + && mkdir -p /opt/edsger
COPY runtime/runner.py /opt/edsger/runner.py
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/bin/python3", "-u", "/opt/edsger/runner.py"]
