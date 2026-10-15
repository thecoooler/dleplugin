<?php

declare(strict_types=1);

it('boots the real DLE runtime', function () {
    global $config, $user_group;
    expect(defined('DATALIFEENGINE'))->toBeTrue();
    expect($config['version_id'])->not->toBeEmpty();
    expect($user_group)->toBeArray()->not->toBeEmpty();
});

it('loads the connected plugin source through DLE', function () {
    require_once DLEPlugins::Check(ENGINE_DIR . '/Acme/integrationsmoke/Probe.php');
    expect(\Acme\integrationsmoke\Probe::answer())->toBe(42);
});

it('applies registered file patches', function () {
    expect(dleplugin_integration_marker())->toBe('patched');
});

it('runs the plugin install SQL against MariaDB', function () {
    global $db;
    $row = $db->super_query('SELECT value FROM ' . PREFIX . '_integration_smoke WHERE id=1');
    expect($row['value'])->toBe('installed');
    $db->query('UPDATE ' . PREFIX . '_integration_smoke SET value="mutated" WHERE id=1');
});
