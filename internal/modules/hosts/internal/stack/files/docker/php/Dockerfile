ARG PHP_VERSION=8.5
FROM php:${PHP_VERSION}-fpm

ARG UID=1000
ARG GID=1000

COPY --from=mlocati/php-extension-installer /usr/bin/install-php-extensions /usr/local/bin/

# Старые образы (7.4 — Debian bullseye) могут быть уже перенесены в archive.debian.org
RUN if ! apt-get update; then \
        sed -i 's|deb.debian.org|archive.debian.org|g; /security/d' /etc/apt/sources.list; \
        echo 'Acquire::Check-Valid-Until "false";' > /etc/apt/apt.conf.d/99archive; \
        apt-get update; \
    fi

# В PHP 8.5 opcache встроен — ставим его только если его нет
RUN install-php-extensions mysqli pdo_mysql gd zip intl exif bcmath soap @composer \
    && (php -m | grep -qi opcache || install-php-extensions opcache) \
    && apt-get update \
    && apt-get install -y --no-install-recommends git unzip mariadb-client nano \
    && rm -rf /var/lib/apt/lists/* \
    && cp "$PHP_INI_DIR/php.ini-development" "$PHP_INI_DIR/php.ini" \
    && groupmod -o -g ${GID} www-data \
    && usermod -o -u ${UID} -g ${GID} www-data

# PHP 7.4 (EOL) уязвим к CVE-2026-109618/109619, исправленного образа нет.
# Частичные меры (zzz-* читаются после смонтированных zz-custom.*):
# - session.upload_progress выключен — подтверждённый путь CVE-2026-109619;
# - тело запроса до 32M — меньше шанс OOM при разборе multipart (CVE-2026-109618);
# - pm.max_requests = 1 — воркер FPM не переносит состояние в следующий запрос.
RUN case "$PHP_VERSION" in 7.4*) \
        printf '%s\n' 'session.upload_progress.enabled = 0' 'post_max_size = 32M' 'upload_max_filesize = 32M' \
            > "$PHP_INI_DIR/conf.d/zzz-eol-cve.ini" \
        && printf '%s\n' '[www]' 'pm.max_requests = 1' > /usr/local/etc/php-fpm.d/zzz-eol-cve.conf ;; \
    esac

WORKDIR /var/www
