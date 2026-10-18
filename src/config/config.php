<?php
return [
    'db' => [
        'host' => getenv('DNS_DB_HOST') ?: '127.0.0.1',
        'port' => (int)(getenv('DNS_DB_PORT') ?: 3306),
        'name' => getenv('DNS_DB_NAME') ?: 'dns_manager',
        'user' => getenv('DNS_DB_USER') ?: '',
        'pass' => getenv('DNS_DB_PASS') ?: '',
        'charset' => 'utf8mb4',
    ],
    'redis' => [
        'host' => getenv('DNS_REDIS_HOST') ?: '127.0.0.1',
        'port' => (int)(getenv('DNS_REDIS_PORT') ?: 6379),
        'auth' => getenv('DNS_REDIS_AUTH') ?: '',
        'db' => 0,
        'prefix' => 'dns:',
    ],
    'app' => [
        'secret_key' => getenv('DNS_SECRET_KEY') ?: '',
        'cron_key' => getenv('DNS_CRON_KEY') ?: '',
        'captcha_scene' => getenv('DNS_CAPTCHA_SCENE') ?: '',
        'captcha_prefix' => getenv('DNS_CAPTCHA_PREFIX') ?: '',
        'session_ttl' => 86400,
        'max_retry' => 5,
        'retry_base_delay' => 60, // 秒，指数退避: delay * 2^retry
        'timezone' => 'Asia/Shanghai',
    ],
];
