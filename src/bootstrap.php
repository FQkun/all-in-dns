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
if (strlen((string)($GLOBALS['config']['app']['secret_key'] ?? '')) < 32) {
    if (PHP_SAPI === 'cli') {
        fwrite(STDERR, "系统密钥未配置（config.php secret_key）\n");
        exit(1);
    }
    http_response_code(500);
    exit('系统密钥未配置');
}
date_default_timezone_set($GLOBALS['config']['app']['timezone']);
