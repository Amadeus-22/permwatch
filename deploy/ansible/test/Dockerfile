# A Debian host with systemd and Python, for testing the role in a container.
FROM debian:12-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends systemd systemd-sysv python3 ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && rm -f /lib/systemd/system/multi-user.target.wants/* /etc/systemd/system/*.wants/* \
          /lib/systemd/system/sockets.target.wants/*udev* /lib/systemd/system/sockets.target.wants/*initctl*
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
