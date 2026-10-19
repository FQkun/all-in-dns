<?php
namespace App\Core;

class Database extends \PDO
{
    private static ?Database $instance = null;

    public static function i(): self
    {
        if (!self::$instance) {
            $c = $GLOBALS['config']['db'];
            self::$instance = new self(
                "mysql:host={$c['host']};port={$c['port']};dbname={$c['name']};charset={$c['charset']}",
                $c['user'],
                $c['pass'],
                [\PDO::ATTR_ERRMODE => \PDO::ERRMODE_EXCEPTION,
                 \PDO::ATTR_DEFAULT_FETCH_MODE => \PDO::FETCH_ASSOC,
                 \PDO::ATTR_EMULATE_PREPARES => false]
            );
            self::$instance->exec('SET SESSION group_concat_max_len = 8192');
        }
        return self::$instance;
    }
}
