<?php
// Форма входа dlehost перед сайтом (dlehost protect). Служебный файл стека:
// перезаписывается при обновлении dlehost, не редактируйте его вручную.
//
// Apache пропускает к сайту только запросы с действующей cookie сессии:
// файл <GATE_DIR>/<сайт>/sha1(<cookie>)-v2-sha256(<bcrypt-хеш>) существует, а срок в
// значении не истёк (см. vhost 60-host-<сайт>.conf). Этот скрипт проверяет
// отдельный пароль по bcrypt-хешу protect, создаёт и удаляет файлы сессий.
//
// Имя cookie зависит от схемы так же, как в vhost: на HTTPS (HTTPS=on от
// ProxyFCGISetEnvIf публичных HTTPS-хостов) — только Secure __Host-dlehost_gate,
// иначе — dlehost_gate. Срок сессии — DLEHOST_GATE_TTL из vhost (секунды,
// 0 — без серверного истечения).

declare(strict_types=1);

const GATE_DIR = '/var/lib/dlehost/gate';
const PASSWORDS_FILE = '/opt/dlehost/protect/passwords.json';
const NO_EXPIRY = '99991231235959'; // срок «без истечения» в формате %{TIME}
const COOKIE_MAX_AGE = 400 * 24 * 3600; // браузеры не хранят cookie дольше ~400 дней
const MAX_FAILURES = 20;   // неудачных попыток на сайт…
const FAILURE_WINDOW = 900; // …за 15 минут
const LOGIN_PATH = '/__dlehost/login';
const LOGOUT_PATH = '/__dlehost/logout';

header('Cache-Control: no-store');
header("Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'");
header('X-Frame-Options: DENY');
header('X-Content-Type-Options: nosniff');
header('Referrer-Policy: no-referrer');
header('X-Robots-Tag: noindex, nofollow');

$site = (string)($_SERVER['DLEHOST_GATE_SITE'] ?? '');
$ttl = (string)($_SERVER['DLEHOST_GATE_TTL'] ?? '');
if (!preg_match('/^[a-z0-9]([a-z0-9-]{0,40}[a-z0-9])?$/', $site) || !preg_match('/^[0-9]{1,12}$/', $ttl)) {
    fail(500, 'Вход не настроен');
}
$ttl = (int)$ttl;
$secure = ($_SERVER['HTTPS'] ?? '') === 'on';
$sessionCookie = $secure ? '__Host-dlehost_gate' : 'dlehost_gate';
$csrfCookie = $secure ? '__Host-dlehost_gate_csrf' : 'dlehost_gate_csrf';
$dir = GATE_DIR . '/' . $site;
// Единственный снимок хеша на запрос: смена пароля меняет пространство сессий.
$config = json_decode((string)@file_get_contents(PASSWORDS_FILE), true);
$hash = is_array($config) ? ($config['hosts'][$site] ?? $config['global'] ?? null) : null;
if (!is_string($hash) || !preg_match('/^\$2[aby]\$10\$[.\/A-Za-z0-9]{53}$/', $hash)) {
    $hash = null;
}
$sessionSuffix = '-v2-' . hash('sha256', $hash ?? '');
$method = $_SERVER['REQUEST_METHOD'] ?? 'GET';
$requestURI = (string)($_SERVER['REQUEST_URI'] ?? '/');
$path = rawurldecode((string)parse_url($requestURI, PHP_URL_PATH));

if ($path === LOGOUT_PATH) {
    logout();
} elseif ($path === LOGIN_PATH) {
    login();
} else {
    // Сюда Apache направляет запросы к сайту без действующей сессии.
    $code = $method === 'GET' || $method === 'HEAD' ? 302 : 303;
    redirect(LOGIN_PATH . '?return=' . rawurlencode(safeReturn($requestURI)), $code);
}

function login(): void
{
    global $method, $dir;
    $return = safeReturn($_POST['return'] ?? $_GET['return'] ?? '/');
    if (currentSession() !== null) {
        redirect($return, 303);
    }
    if ($method !== 'POST') {
        form($return, 200, '');
    }
    if (!sameOrigin() || !validCSRF()) {
        form($return, 403, 'Форма устарела, попробуйте ещё раз');
    }
    $password = (string)($_POST['password'] ?? '');

    ensureDir($dir);
    $lock = fopen($dir . '/attempts', 'c+');
    if ($lock === false || !flock($lock, LOCK_EX)) {
        fail(503, 'Вход временно недоступен');
    }
    // Проверки сериализованы блокировкой: параллельные запросы не обходят лимит.
    $now = time();
    $failures = array_values(array_filter(
        array_map('intval', explode("\n", (string)stream_get_contents($lock))),
        fn(int $t): bool => $t > $now - FAILURE_WINDOW
    ));
    if (count($failures) >= MAX_FAILURES) {
        header('Retry-After: ' . max(1, $failures[0] + FAILURE_WINDOW - $now));
        form($return, 429, 'Слишком много попыток, повторите позже');
    }
    $hash = $GLOBALS['hash'];
    // Фиктивная проверка той же стоимости при отсутствии/повреждении хеша.
    $verified = password_verify(substr($password, 0, 72), $hash ?? '$2a$10$pMVK93Fjc2/Dp9KllbU9Ju1StdQUNCkzO1nXCik70k9DYSVYoby72');
    if ($hash === null || $password === '' || strlen($password) > 72 || !$verified) {
        $failures[] = $now;
        ftruncate($lock, 0);
        rewind($lock);
        fwrite($lock, implode("\n", $failures) . "\n");
        fflush($lock);
        usleep(300000);
        form($return, 401, 'Неверный пароль');
    }
    flock($lock, LOCK_UN);
    fclose($lock);

    // Новый идентификатор при каждом входе; прежняя сессия браузера удаляется.
    deleteSession($GLOBALS['sessionCookie']);
    pruneSessions();
    $ttl = $GLOBALS['ttl'];
    // Срок сервера и cookie совпадают; без истечения cookie живёт сколько
    // позволит браузер (до ~400 дней), затем нужен повторный вход.
    $expiry = $ttl > 0 ? gmdate('YmdHis', $now + $ttl) : NO_EXPIRY;
    $cookieExpires = $now + ($ttl > 0 ? min($ttl, COOKIE_MAX_AGE) : COOKIE_MAX_AGE);
    $value = $expiry . '-' . rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
    $file = sessionFile($value);
    // В файле только срок для очистки; ни логина, ни пароля, ни хеша.
    if (file_put_contents($file, $expiry . "\n", LOCK_EX) === false) {
        fail(503, 'Не удалось создать сессию');
    }
    @chmod($file, 0600);
    setGateCookie($GLOBALS['sessionCookie'], $value, $cookieExpires, 'Lax');
    redirect($return, 303);
}

function logout(): void
{
    global $method;
    $session = currentSession();
    if ($method === 'POST') {
        // Выход — только из формы этого сайта: токен выводится из самой сессии.
        if ($session !== null && (!sameOrigin() || !hash_equals(logoutToken($session), (string)($_POST['token'] ?? '')))) {
            fail(403, 'Форма устарела, попробуйте ещё раз');
        }
        // Завершаются сессии под обоими именами cookie, которые прислал браузер:
        // не-Secure cookie (вход по HTTP) отправляется и по HTTPS.
        foreach (['dlehost_gate', '__Host-dlehost_gate'] as $name) {
            deleteSession($name);
            if (isset($_COOKIE[$name]) && ($GLOBALS['secure'] || $name === 'dlehost_gate')) {
                setGateCookie($name, '', 1, 'Lax');
            }
        }
        redirect(LOGIN_PATH, 303);
    }
    if ($session === null) {
        page(200, 'Вы не вошли', '<p><a href="' . LOGIN_PATH . '">Войти</a></p>');
    }
    page(200, 'Выход', '<form method="post" action="' . LOGOUT_PATH . '">'
        . '<input type="hidden" name="token" value="' . h(logoutToken($session)) . '">'
        . '<button type="submit">Выйти</button></form>');
}

function currentSession(): ?string
{
    if ($GLOBALS['hash'] === null) {
        return null;
    }
    $value = (string)($_COOKIE[$GLOBALS['sessionCookie']] ?? '');
    if (!preg_match('/^([0-9]{14})-[A-Za-z0-9_-]{43}$/', $value, $m) || $m[1] <= gmdate('YmdHis')) {
        return null;
    }
    return is_file(sessionFile($value)) ? $value : null;
}

function deleteSession(string $cookie): void
{
    $value = (string)($_COOKIE[$cookie] ?? '');
    if (preg_match('/^[0-9]{14}-[A-Za-z0-9_-]{43}$/', $value)) {
        @unlink(sessionFile($value));
    }
}

function sessionFile(string $value): string
{
    return $GLOBALS['dir'] . '/' . sha1($value) . $GLOBALS['sessionSuffix'];
}

// Удаляет файлы истёкших сессий; срок — первая строка файла.
function pruneSessions(): void
{
    $now = gmdate('YmdHis');
    foreach (glob($GLOBALS['dir'] . '/' . str_repeat('[0-9a-f]', 40) . '-v2-*') ?: [] as $file) {
        $expiry = (string)@file_get_contents($file, false, null, 0, 14);
        if (preg_match('/^[0-9]{14}$/', $expiry) && $expiry <= $now) {
            @unlink($file);
        }
    }
}

function logoutToken(string $session): string
{
    return hash_hmac('sha256', 'logout', $session);
}

function validCSRF(): bool
{
    $cookie = (string)($_COOKIE[$GLOBALS['csrfCookie']] ?? '');
    return strlen($cookie) === 43 && hash_equals($cookie, (string)($_POST['csrf'] ?? ''));
}

// Браузеры передают Origin в POST; чужой Origin или межсайтовый запрос отклоняются.
function sameOrigin(): bool
{
    if (($_SERVER['HTTP_SEC_FETCH_SITE'] ?? 'same-origin') === 'cross-site') {
        return false;
    }
    $origin = $_SERVER['HTTP_ORIGIN'] ?? null;
    if ($origin === null) {
        return true;
    }
    $host = parse_url($origin, PHP_URL_HOST);
    $port = parse_url($origin, PHP_URL_PORT);
    if (!is_string($host)) {
        return false;
    }
    $authority = $host . ($port !== null ? ':' . $port : '');
    return strcasecmp($authority, (string)($_SERVER['HTTP_HOST'] ?? '')) === 0;
}

function ensureDir(string $dir): void
{
    if (!is_dir($dir) && !@mkdir($dir, 0700) && !is_dir($dir)) {
        fail(503, 'Вход временно недоступен');
    }
}

// Возврат только на локальный путь этого сайта.
function safeReturn($value): string
{
    if (!is_string($value) || $value === '' || strlen($value) > 4096 || $value[0] !== '/'
        || preg_match('/[\x00-\x20\x7f\\\\]/', $value) || strncmp($value, '//', 2) === 0
        || strncmp(rawurldecode($value), '/__dlehost/', 11) === 0) {
        return '/';
    }
    return $value;
}

function setGateCookie(string $name, string $value, int $expires, string $sameSite): void
{
    setcookie($name, $value, [
        'expires' => $expires,
        'path' => '/',
        'secure' => $GLOBALS['secure'],
        'httponly' => true,
        'samesite' => $sameSite,
    ]);
}

function csrfToken(): string
{
    $token = (string)($_COOKIE[$GLOBALS['csrfCookie']] ?? '');
    if (!preg_match('/^[A-Za-z0-9_-]{43}$/', $token)) {
        $token = rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
        setGateCookie($GLOBALS['csrfCookie'], $token, 0, 'Strict');
    }
    return $token;
}

function form(string $return, int $code, string $error): void
{
    $body = ($error !== '' ? '<p class="error">' . h($error) . '</p>' : '')
        . '<form method="post" action="' . LOGIN_PATH . '">'
        . '<input type="hidden" name="csrf" value="' . h(csrfToken()) . '">'
        . '<input type="hidden" name="return" value="' . h($return) . '">'
        . '<label>Пароль<input name="password" type="password" autocomplete="current-password" required autofocus></label>'
        . '<button type="submit">Войти</button></form>'
        . '<p class="hint">Пароль доступа к сайту.</p>';
    page($code, 'Вход на сайт', $body);
}

function page(int $code, string $title, string $body): void
{
    http_response_code($code);
    header('Content-Type: text/html; charset=utf-8');
    echo '<!doctype html><html lang="ru"><head><meta charset="utf-8">'
        . '<meta name="viewport" content="width=device-width, initial-scale=1">'
        . '<meta name="robots" content="noindex, nofollow"><title>' . h($title) . '</title><style>'
        . 'body{font:16px/1.4 system-ui,sans-serif;background:#f3f4f6;margin:0;display:flex;min-height:100vh;align-items:center;justify-content:center}'
        . 'main{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 1px 4px #0002;width:min(20rem,90vw)}'
        . 'h1{font-size:1.3rem;margin:0 0 1rem}label{display:block;margin:0 0 .8rem}'
        . 'input{display:block;box-sizing:border-box;width:100%;padding:.5rem;margin-top:.2rem;font:inherit}'
        . 'button{width:100%;padding:.6rem;font:inherit;cursor:pointer}.error{color:#b91c1c}.hint{color:#6b7280;font-size:.85rem}'
        . '</style></head><body><main><h1>' . h($title) . '</h1>' . $body . '</main></body></html>';
    exit;
}

function fail(int $code, string $message): void
{
    page($code, $message, '');
}

function redirect(string $location, int $code): void
{
    header('Location: ' . $location, true, $code);
    exit;
}

function h(string $value): string
{
    return htmlspecialchars($value, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
}
