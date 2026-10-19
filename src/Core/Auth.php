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

    private static function isSecure(): bool
    {
        if (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off') return true;
        return strtolower((string)($_SERVER['HTTP_X_FORWARDED_PROTO'] ?? '')) === 'https';
    }

    public static function login(array $user): string
    {
        $sid = bin2hex(random_bytes(32));
        $ttl = $GLOBALS['config']['app']['session_ttl'];
        RedisClient::i()->set('session:' . $sid, json_encode(['id' => (int)$user['id']]), $ttl);
        setcookie('DNS_SID', $sid, ['expires' => time() + $ttl, 'path' => '/', 'secure' => self::isSecure(), 'httponly' => true, 'samesite' => 'Strict']);
        return $sid;
    }

    public static function logout(): void
    {
        $sid = $_COOKIE['DNS_SID'] ?? '';
        if ($sid) RedisClient::i()->del('session:' . $sid);
        setcookie('DNS_SID', '', ['expires' => time() - 3600, 'path' => '/', 'secure' => self::isSecure(), 'httponly' => true, 'samesite' => 'Strict']);
    }

    public static function user(): ?array
    {
        $sid = $_COOKIE['DNS_SID'] ?? '';
        if (!$sid) return null;
        try {
            $raw = RedisClient::i()->get('session:' . $sid);
        } catch (\Throwable $e) {
            error_log('[AUTH] redis unavailable: ' . $e->getMessage());
            Http::json(['code' => 503, 'msg' => '服务暂时不可用（会话存储异常），请稍后重试'], 503);
        }
        if (!$raw) return null;
        $sess = json_decode($raw, true);
        $uid = (int)($sess['id'] ?? 0);
        if (!$uid) return null;
        try {
            $st = Database::i()->prepare('SELECT id,username,role,last_login_at FROM users WHERE id=?');
            $st->execute([$uid]);
            return $st->fetch() ?: null;
        } catch (\Throwable $e) {
            return null;
        }
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
