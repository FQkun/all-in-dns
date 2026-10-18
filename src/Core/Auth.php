<?php
namespace App\Core;

class Auth
{
    public static function attempt(string $username, string $password): ?array
    {
        $db = Database::i();
        $st = $db->prepare('SELECT * FROM users WHERE username=?');
        $st->execute([$username]);
        $u = $st->fetch();
        if (!$u || !password_verify($password, $u['password_hash'])) return null;
        $db->prepare('UPDATE users SET last_login_at=NOW() WHERE id=?')->execute([$u['id']]);
        unset($u['password_hash']);
        return $u;
    }

    public static function login(array $user): string
    {
        $sid = bin2hex(random_bytes(32));
        $ttl = $GLOBALS['config']['app']['session_ttl'];
        RedisClient::i()->set('session:' . $sid, json_encode($user), $ttl);
        setcookie('DNS_SID', $sid, ['expires' => time() + $ttl, 'path' => '/', 'secure' => !empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off', 'httponly' => true, 'samesite' => 'Strict']);
        return $sid;
    }

    public static function logout(): void
    {
        $sid = $_COOKIE['DNS_SID'] ?? '';
        if ($sid) RedisClient::i()->del('session:' . $sid);
        setcookie('DNS_SID', '', ['expires' => time() - 3600, 'path' => '/', 'secure' => !empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off', 'httponly' => true, 'samesite' => 'Strict']);
    }

    public static function user(): ?array
    {
        $sid = $_COOKIE['DNS_SID'] ?? '';
        if (!$sid) return null;
        $raw = RedisClient::i()->get('session:' . $sid);
        return $raw ? json_decode($raw, true) : null;
    }

    public static function requireUser(): array
    {
        $u = self::user();
        if (!$u) Http::json(['code' => 401, 'msg' => '未登录'], 401);
        return $u;
    }

    public static function requireRole(string ...$roles): array
    {
        $u = self::requireUser();
        if (!in_array($u['role'], $roles, true)) Http::json(['code' => 403, 'msg' => '无权限'], 403);
        return $u;
    }
}
