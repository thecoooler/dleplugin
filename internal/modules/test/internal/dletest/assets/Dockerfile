FROM php:8.4-cli@sha256:1d2cf2b7d715b5b3aa959f0150ccbcc7b71e0181b637e443abf40a7fb56bf79a

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        unzip libfreetype6-dev libicu-dev libjpeg62-turbo-dev libpng-dev libzip-dev \
    && docker-php-ext-configure gd --with-freetype --with-jpeg \
    && docker-php-ext-install -j"$(nproc)" exif gd intl mysqli zip \
    && pecl install pcov-1.0.12 \
    && docker-php-ext-enable pcov \
    && echo "pcov.enabled=0" > /usr/local/etc/php/conf.d/zz-pcov.ini \
    && rm -rf /var/lib/apt/lists/* /tmp/pear

COPY --from=composer:2@sha256:af98f42dfff7c68ba8d53c2164fd9fde1087b7d449514baa38c418b1f6bc4bac /usr/bin/composer /usr/bin/composer

ENV COMPOSER_HOME=/tmp/composer
# Tests run in /dle: the DLE copy is mounted next to vendor/ so that Pest
# treats /dle as the project root (tests/, snapshots).
WORKDIR /dle
COPY composer.json composer.lock ./
# Pest writes coverage here even when tests run as the host user's UID.
RUN composer install --no-dev --no-interaction --prefer-dist --no-progress \
    && mkdir -p tests vendor/pestphp/pest/.temp \
    && chmod 1777 vendor/pestphp/pest/.temp \
    && rm -rf /tmp/composer
