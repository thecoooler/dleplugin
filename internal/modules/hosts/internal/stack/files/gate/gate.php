<?php
// Login form in front of a site (dleplugin hosts protect). A stack service file:
// overwritten when dleplugin is updated, do not edit it by hand.
//
// Apache lets through to the site only requests with a valid session cookie:
// the file <GATE_DIR>/<site>/sha1(<cookie>)-v2-sha256(<bcrypt hash>) exists and the
// expiry in the value has not passed (see vhost 60-host-<site>.conf). This script
// checks the separate password against the protect bcrypt hash and creates and
// removes the session files.
//
// The cookie name depends on the scheme as in the vhost: over HTTPS (HTTPS=on from
// ProxyFCGISetEnvIf of public HTTPS hosts) only the Secure __Host-dleplugin_hosts_gate,
// otherwise dleplugin_hosts_gate. The session lifetime is DLEPLUGIN_HOSTS_GATE_TTL from the vhost
// (seconds, 0 means no server-side expiry).

declare(strict_types=1);

const GATE_DIR = '/var/lib/dleplugin/hosts/gate';
const PASSWORDS_FILE = '/opt/dleplugin/hosts/protect/passwords.json';
const NO_EXPIRY = '99991231235959'; // "no expiry" in the %{TIME} format
const COOKIE_MAX_AGE = 400 * 24 * 3600; // browsers keep cookies for ~400 days at most
const MAX_FAILURES = 20;   // failed attempts per site…
const FAILURE_WINDOW = 900; // …within 15 minutes
const LOGIN_PATH = '/__dleplugin/login';
const LOGOUT_PATH = '/__dleplugin/logout';

header('Cache-Control: no-store');
header("Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'");
header('X-Frame-Options: DENY');
header('X-Content-Type-Options: nosniff');
header('Referrer-Policy: no-referrer');
header('X-Robots-Tag: noindex, nofollow');

$site = (string)($_SERVER['DLEPLUGIN_HOSTS_GATE_SITE'] ?? '');
$ttl = (string)($_SERVER['DLEPLUGIN_HOSTS_GATE_TTL'] ?? '');
if (!preg_match('/^[a-z0-9]([a-z0-9-]{0,40}[a-z0-9])?$/', $site) || !preg_match('/^[0-9]{1,12}$/', $ttl)) {
    fail(500, 'Login is not configured');
}
$ttl = (int)$ttl;
$secure = ($_SERVER['HTTPS'] ?? '') === 'on';
$sessionCookie = $secure ? '__Host-dleplugin_hosts_gate' : 'dleplugin_hosts_gate';
$csrfCookie = $secure ? '__Host-dleplugin_hosts_gate_csrf' : 'dleplugin_hosts_gate_csrf';
$dir = GATE_DIR . '/' . $site;
// One hash snapshot per request: a password change changes the session namespace.
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
    // Apache sends site requests without a valid session here.
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
        form($return, 403, 'The form has expired, please try again');
    }
    $password = (string)($_POST['password'] ?? '');

    ensureDir($dir);
    $lock = fopen($dir . '/attempts', 'c+');
    if ($lock === false || !flock($lock, LOCK_EX)) {
        fail(503, 'Login is temporarily unavailable');
    }
    // Checks are serialized by the lock: parallel requests do not bypass the limit.
    $now = time();
    $failures = array_values(array_filter(
        array_map('intval', explode("\n", (string)stream_get_contents($lock))),
        fn(int $t): bool => $t > $now - FAILURE_WINDOW
    ));
    if (count($failures) >= MAX_FAILURES) {
        header('Retry-After: ' . max(1, $failures[0] + FAILURE_WINDOW - $now));
        form($return, 429, 'Too many attempts, try again later');
    }
    $hash = $GLOBALS['hash'];
    // A dummy check of the same cost when the hash is missing or broken.
    $verified = password_verify(substr($password, 0, 72), $hash ?? '$2a$10$pMVK93Fjc2/Dp9KllbU9Ju1StdQUNCkzO1nXCik70k9DYSVYoby72');
    if ($hash === null || $password === '' || strlen($password) > 72 || !$verified) {
        $failures[] = $now;
        ftruncate($lock, 0);
        rewind($lock);
        fwrite($lock, implode("\n", $failures) . "\n");
        fflush($lock);
        usleep(300000);
        form($return, 401, 'Wrong password');
    }
    flock($lock, LOCK_UN);
    fclose($lock);

    // A new identifier on every login; the previous browser session is removed.
    deleteSession($GLOBALS['sessionCookie']);
    pruneSessions();
    $ttl = $GLOBALS['ttl'];
    // The server and cookie expiries match; without expiry the cookie lives as
    // long as the browser allows (up to ~400 days), then a new login is needed.
    $expiry = $ttl > 0 ? gmdate('YmdHis', $now + $ttl) : NO_EXPIRY;
    $cookieExpires = $now + ($ttl > 0 ? min($ttl, COOKIE_MAX_AGE) : COOKIE_MAX_AGE);
    $value = $expiry . '-' . rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '=');
    $file = sessionFile($value);
    // The file holds only the expiry for cleanup; no login, password or hash.
    if (file_put_contents($file, $expiry . "\n", LOCK_EX) === false) {
        fail(503, 'Could not create a session');
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
        // Logout only from this site's form: the token is derived from the session itself.
        if ($session !== null && (!sameOrigin() || !hash_equals(logoutToken($session), (string)($_POST['token'] ?? '')))) {
            fail(403, 'The form has expired, please try again');
        }
        // End the sessions under both cookie names the browser sent:
        // a non-Secure cookie (HTTP login) is also sent over HTTPS.
        foreach (['dleplugin_hosts_gate', '__Host-dleplugin_hosts_gate'] as $name) {
            deleteSession($name);
            if (isset($_COOKIE[$name]) && ($GLOBALS['secure'] || $name === 'dleplugin_hosts_gate')) {
                setGateCookie($name, '', 1, 'Lax');
            }
        }
        redirect(LOGIN_PATH, 303);
    }
    if ($session === null) {
        page(200, 'You are not logged in', '<p><a href="' . LOGIN_PATH . '">Log in</a></p>');
    }
    page(200, 'Log out', '<form method="post" action="' . LOGOUT_PATH . '">'
        . '<input type="hidden" name="token" value="' . h(logoutToken($session)) . '">'
        . '<button type="submit">Log out</button></form>');
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

// Removes expired session files; the expiry is the first line of the file.
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

// Browsers send Origin with POST; a foreign Origin or a cross-site request is rejected.
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
        fail(503, 'Login is temporarily unavailable');
    }
}

// Return only to a local path of this site.
function safeReturn($value): string
{
    if (!is_string($value) || $value === '' || strlen($value) > 4096 || $value[0] !== '/'
        || preg_match('/[\x00-\x20\x7f\\\\]/', $value) || strncmp($value, '//', 2) === 0
        || strncmp(rawurldecode($value), '/__dleplugin/', strlen('/__dleplugin/')) === 0) {
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
        . '<label>Password<input name="password" type="password" autocomplete="current-password" required autofocus></label>'
        . '<button type="submit">Log in</button></form>'
        . '<p class="hint">Site access password.</p>';
    page($code, 'Site login', $body);
}

function page(int $code, string $title, string $body): void
{
    http_response_code($code);
    header('Content-Type: text/html; charset=utf-8');
    echo '<!doctype html><html lang="en"><head><meta charset="utf-8">'
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
