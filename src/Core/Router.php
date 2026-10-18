<?php
namespace App\Core;

class Router
{
    private array $routes = [];

    public function add(string $method, string $path, callable $handler): void
    {
        $this->routes[] = [$method, $path, $handler];
    }

    public function get(string $p, callable $h): void { $this->add('GET', $p, $h); }
    public function post(string $p, callable $h): void { $this->add('POST', $p, $h); }
    public function delete(string $p, callable $h): void { $this->add('DELETE', $p, $h); }

    public function dispatch(?string $uri = null): void
    {
        $uri = $uri ?? parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);
        foreach ($this->routes as [$method, $path, $handler]) {
            if ($method !== $_SERVER['REQUEST_METHOD']) continue;
            $pattern = '#^' . preg_replace('#\{(\w+)\}#', '(?P<$1>[^/]+)', $path) . '$#';
            if (preg_match($pattern, $uri, $m)) {
                $params = array_filter($m, 'is_string', ARRAY_FILTER_USE_KEY);
                try {
                    $handler(array_values($params)[0] ?? null);
                } catch (\Throwable $e) {
                    $requestId = bin2hex(random_bytes(8));
                    error_log(sprintf('[%s] %s in %s:%d', $requestId, $e->getMessage(), $e->getFile(), $e->getLine()));
                    Http::json(['code' => 500, 'msg' => '服务器内部错误', 'request_id' => $requestId], 500);
                }
                return;
            }
        }
        Http::json(['code' => 404, 'msg' => '接口不存在'], 404);
    }
}
