<?php
namespace App\Core;

class Http
{
    public static function json(array $data, int $status = 200): never
    {
        http_response_code($status);
        header('Content-Type: application/json; charset=utf-8');
        header('X-Time: ' . (int)round((microtime(true) - ($GLOBALS['__start'] ?? microtime(true))) * 1000));
        echo json_encode($data, JSON_UNESCAPED_UNICODE);
        exit;
    }

    public static function ok(mixed $data = null): never
    {
        self::json(['code' => 0, 'msg' => 'ok', 'data' => $data]);
    }

    public static function fail(string $msg, int $status = 400): never
    {
        self::json(['code' => $status, 'msg' => $msg], $status);
    }

    public static function input(): array
    {
        $raw = file_get_contents('php://input');
        $j = json_decode($raw, true);
        if (is_array($j)) return array_merge($_POST, $j);
        return $_POST;
    }

    public static function ip(): string
    {
        return $_SERVER['HTTP_X_REAL_IP'] ?? $_SERVER['REMOTE_ADDR'] ?? '0.0.0.0';
    }
}
