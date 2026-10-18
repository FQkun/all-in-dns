<?php
declare(strict_types=1);

$uri = $_GET['r'] ?? parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);

if (!is_file(__DIR__ . '/../src/config/config.php')) {
    header('Location: /install.php');
    exit;
}

require __DIR__ . '/../src/bootstrap.php';

if (preg_match('#^/assets/#', $uri)) {
    $file = realpath(__DIR__ . $uri);
    if (!$file || !str_starts_with($file, __DIR__) || !is_file($file)) {
        http_response_code(404);
        exit;
    }
    $types = ['css' => 'text/css', 'js' => 'application/javascript', 'svg' => 'image/svg+xml', 'png' => 'image/png'];
    $ext = strtolower(pathinfo($file, PATHINFO_EXTENSION));
    header('Content-Type: ' . ($types[$ext] ?? 'application/octet-stream'));
    readfile($file);
    exit;
}

if (str_starts_with($uri, '/api/')) {
    $router = new App\Core\Router();
    (require __DIR__ . '/../src/api/routes.php')($router);
    $router->dispatch($uri);
    exit;
}

if ($uri !== '/' && $uri !== '/index.php') {
    http_response_code(404);
    echo 'Not Found';
    exit;
}

$dbErr = '';
$installed = false;
try {
    $chk = App\Core\Database::i()->query('SELECT COUNT(*) c FROM users')->fetch();
    $installed = (int)($chk['c'] ?? 0) > 0;
} catch (Throwable $e) {
    $dbErr = $e->getMessage();
}
if (!$installed):
    header('Content-Type: text/html; charset=utf-8');
    if ($dbErr !== '') {
        http_response_code(500);
        echo '<div style="font-family:sans-serif;background:#f5f5f8;color:#1b1c26;padding:40px">'
           . '<h2 style="color:#d93843">数据库连接失败，无法进入系统</h2>'
           . '<p style="color:#6f7080">' . htmlspecialchars($dbErr) . '</p>'
           . '<p style="color:#6f7080">请检查 src/config/config.php 中的数据库配置，或重新运行 install.php</p></div>';
    } else {
        header('Location: /install.php');
    }
    exit;
endif;
?>
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta name="color-scheme" content="light">
<link rel="icon" href="data:,">
<title>DNS 统一管理平台</title>
<link rel="stylesheet" href="/assets/style.css">
</head>
<body>
<div id="app">
  <aside id="sidebar">
    <div class="logo">DNS 管理平台</div>
    <nav id="nav"></nav>
    <div class="sidebar-footer">
      <div class="sf-user"><span id="whoami"></span><button id="btn-logout" class="btn ghost sm">退出</button></div>
      <span id="req-time"></span>
    </div>
  </aside>
  <main id="main">
    <div id="login-view" class="login-wrap hidden">
      <form id="login-form" class="login-card">
        <h2>欢迎回来</h2>
        <input name="username" placeholder="用户名" required autocomplete="username">
        <input name="password" type="password" placeholder="密码" required autocomplete="current-password">
<?php
$__cfg = is_file(__DIR__ . '/../src/config/config.php') ? (require __DIR__ . '/../src/config/config.php') : [];
$__captcha = trim((string)($__cfg['app']['captcha_prefix'] ?? ''));
if ($__captcha !== ''):
?>
<div id="captcha-element"></div>
<?php endif; ?>
<button class="btn primary" type="button" id="login-btn">登录</button>
        <p class="author-line">开源项目 <a href="https://github.com/FQkun/all-in-dns" target="_blank" rel="noopener">FQkun/all-in-dns</a></p>
      </form>
    </div>
    <div id="view" class="hidden"></div>
    <footer id="app-footer"><span>DNS 统一管理平台</span><span>腾讯云 · 阿里云 · 火山引擎 · 三端同步</span><a class="author-link" href="https://github.com/FQkun/all-in-dns" target="_blank" rel="noopener">GitHub @FQkun</a></footer>
  </main>
</div>
<div id="toast"></div>
<div id="modal-root"></div>
<script>
window.AliyunCaptchaConfig = {
  region: "cn",
  scene: "<?= htmlspecialchars($__cfg['app']['captcha_scene'] ?? '') ?>",
  prefix: "<?= htmlspecialchars($__captcha) ?>"
};
</script>
<?php if ($__captcha !== ''): ?>
<script type="text/javascript" src="https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js"></script>
<?php endif; ?>
<script src="/assets/app.js"></script>
</body>
</html>
