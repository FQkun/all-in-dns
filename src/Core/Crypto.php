<?php
namespace App\Core;

class Crypto
{
    private static function key(): string
    {
        return hash('sha256', $GLOBALS['config']['app']['secret_key'], true);
    }

    public static function encrypt(string $plain): string
    {
        $iv = random_bytes(12);
        $tag = '';
        $cipher = openssl_encrypt($plain, 'aes-256-gcm', self::key(), OPENSSL_RAW_DATA, $iv, $tag);
        return base64_encode($iv . $tag . $cipher);
    }

    public static function decrypt(string $enc): string
    {
        $raw = base64_decode($enc);
        $iv = substr($raw, 0, 12);
        $tag = substr($raw, 12, 16);
        $cipher = substr($raw, 28);
        $plain = openssl_decrypt($cipher, 'aes-256-gcm', self::key(), OPENSSL_RAW_DATA, $iv, $tag);
        if ($plain === false) throw new \RuntimeException('解密失败，请检查 secret_key');
        return $plain;
    }
}
