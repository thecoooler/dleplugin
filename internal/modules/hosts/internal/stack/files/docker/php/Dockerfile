ARG PHP_VERSION=8.5
FROM php:${PHP_VERSION}-fpm

ARG UID=1000
ARG GID=1000

COPY --from=mlocati/php-extension-installer /usr/bin/install-php-extensions /usr/local/bin/

# Old images (7.4 is Debian bullseye) may already be moved to archive.debian.org
RUN if ! apt-get update; then \
        sed -i 's|deb.debian.org|archive.debian.org|g; /security/d' /etc/apt/sources.list; \
        echo 'Acquire::Check-Valid-Until "false";' > /etc/apt/apt.conf.d/99archive; \
        apt-get update; \
    fi

# PHP 8.5 has opcache built in; install it only when missing
RUN install-php-extensions mysqli pdo_mysql gd zip intl exif bcmath soap @composer \
    && (php -m | grep -qi opcache || install-php-extensions opcache) \
    && apt-get update \
    && apt-get install -y --no-install-recommends git unzip mariadb-client nano \
    && rm -rf /var/lib/apt/lists/* \
    && cp "$PHP_INI_DIR/php.ini-development" "$PHP_INI_DIR/php.ini" \
    && groupmod -o -g ${GID} www-data \
    && usermod -o -u ${UID} -g ${GID} www-data

# PHP 7.4 (EOL) is vulnerable to CVE-2026-109618/109619; there is no fixed image.
# Partial mitigations (zzz-* are read after the mounted zz-custom.*):
# - session.upload_progress is off: the confirmed CVE-2026-109619 path;
# - request body up to 32M: less chance of OOM while parsing multipart (CVE-2026-109618);
# - pm.max_requests = 1: an FPM worker does not carry state to the next request.
RUN case "$PHP_VERSION" in 7.4*) \
        printf '%s\n' 'session.upload_progress.enabled = 0' 'post_max_size = 32M' 'upload_max_filesize = 32M' \
            > "$PHP_INI_DIR/conf.d/zzz-eol-cve.ini" \
        && printf '%s\n' '[www]' 'pm.max_requests = 1' > /usr/local/etc/php-fpm.d/zzz-eol-cve.conf ;; \
    esac

WORKDIR /var/www
