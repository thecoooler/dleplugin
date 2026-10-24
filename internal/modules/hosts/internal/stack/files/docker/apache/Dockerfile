FROM debian:trixie-slim

ARG UID=1000
ARG GID=1000

RUN apt-get update && apt-get install -y --no-install-recommends apache2 \
    && rm -rf /var/lib/apt/lists/* \
    && a2enmod proxy proxy_fcgi rewrite headers expires deflate vhost_alias macro setenvif \
    && echo "ServerName localhost" > /etc/apache2/conf-enabled/servername.conf \
    && groupmod -o -g ${GID} www-data \
    && usermod -o -u ${UID} -g ${GID} www-data

EXPOSE 80
CMD ["apache2ctl", "-D", "FOREGROUND"]
