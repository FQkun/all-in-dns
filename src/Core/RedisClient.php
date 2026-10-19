<?php
namespace App\Core;

// 基于 socket 的 RESP 客户端，无外部依赖
class RedisClient
{
    private static ?RedisClient $instance = null;
    private $sock;

    public static function i(): self
    {
        if (!self::$instance) self::$instance = new self();
        return self::$instance;
    }

    public function __construct()
    {
        $c = $GLOBALS['config']['redis'];
        $this->sock = @stream_socket_client("tcp://{$c['host']}:{$c['port']}", $errno, $errstr, 3);
        if (!$this->sock) throw new \RuntimeException("Redis connect failed: $errstr");
        stream_set_timeout($this->sock, 3);
        if (!empty($c['auth'])) $this->cmd('AUTH', $c['auth']);
        if ($c['db']) $this->cmd('SELECT', (string)$c['db']);
    }

    private function cmd(string ...$args): mixed
    {
        $out = '*' . count($args) . "\r\n";
        foreach ($args as $a) $out .= '$' . strlen($a) . "\r\n" . $a . "\r\n";
        fwrite($this->sock, $out);
        return $this->read();
    }

    private function read(): mixed
    {
        $line = fgets($this->sock);
        if ($line === false) throw new \RuntimeException('Redis read failed');
        $type = $line[0];
        $data = substr($line, 1, -2);
        switch ($type) {
            case '+': return $data;
            case '-': throw new \RuntimeException("Redis error: $data");
            case ':': return (int)$data;
            case '$':
                if ($data === '-') return null;
                $len = (int)$data;
                $val = '';
                while (strlen($val) < $len + 2) {
                    $chunk = fread($this->sock, $len + 2 - strlen($val));
                    if ($chunk === false || $chunk === '') {
                        $meta = stream_get_meta_data($this->sock);
                        if (!empty($meta['timed_out']) || feof($this->sock)) throw new \RuntimeException('Redis read timeout/closed');
                        continue;
                    }
                    $val .= $chunk;
                }
                return substr($val, 0, $len);
            case '*':
                $n = (int)$data;
                $arr = [];
                for ($i = 0; $i < $n; $i++) $arr[] = $this->read();
                return $arr;
        }
        throw new \RuntimeException('Redis protocol error');
    }

    public function get(string $k): ?string { $p = $GLOBALS['config']['redis']['prefix']; return $this->cmd('GET', $p . $k); }
    public function set(string $k, string $v, ?int $ttl = null): void
    {
        $p = $GLOBALS['config']['redis']['prefix'];
        $ttl ? $this->cmd('SET', $p . $k, $v, 'EX', (string)$ttl) : $this->cmd('SET', $p . $k, $v);
    }
    public function incr(string $k, ?int $ttl = null): int
    {
        $p = $GLOBALS['config']['redis']['prefix'];
        $n = $this->cmd('INCR', $p . $k);
        if ($n === 1 && $ttl) $this->cmd('EXPIRE', $p . $k, (string)$ttl);
        return $n;
    }
    public function del(string ...$keys): void
    {
        $p = $GLOBALS['config']['redis']['prefix'];
        $this->cmd('DEL', ...array_map(fn($k) => $p . $k, $keys));
    }
}
