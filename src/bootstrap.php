<?php
declare(strict_types=1);
$GLOBALS['__start'] = microtime(true);
spl_autoload_register(function ($class) {
    $prefix = 'App\\';
    if (strncmp($class, $prefix, 4) !== 0) return;
    $file = __DIR__ . '/' . str_replace('\\', '/', substr($class, 4)) . '.php';
    if (is_file($file)) require $file;
});
$GLOBALS['config'] = require __DIR__ . '/config/config.php';
if (PHP_SAPI !== 'cli' && basename((string)($_SERVER['SCRIPT_FILENAME'] ?? '')) !== 'install.php' && strlen((string)($GLOBALS['config']['app']['secret_key'] ?? '')) < 32) {
    http_response_code(500);
    exit('系统密钥未配置');
}
date_default_timezone_set($GLOBALS['config']['app']['timezone']);
