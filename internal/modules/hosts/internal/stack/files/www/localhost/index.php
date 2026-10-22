<?php
echo '<h2>PHP ' . PHP_VERSION . '</h2>';
try {
    $pdo = new PDO('mysql:host=' . getenv('DB_HOST') . ';dbname=' . getenv('DB_NAME') . ';charset=utf8mb4',
        getenv('DB_USER'), getenv('DB_PASSWORD'));
    echo '<p>MariaDB: OK, version ' . $pdo->query('SELECT VERSION()')->fetchColumn() . '</p>';
} catch (Throwable $e) {
    echo '<p>MariaDB: error: ' . htmlspecialchars($e->getMessage()) . '</p>';
}
phpinfo();
