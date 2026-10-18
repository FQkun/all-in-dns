<?php
declare(strict_types=1);

// ========== 已安装检测 ==========
$root = dirname(__DIR__);
$configFile = $root . '/src/config/config.php';
$installed = false;
if (is_file($configFile)) {
    $cfg = @require $configFile;
    if (is_array($cfg)) {
        try {
            $pdo = new PDO(
                "mysql:host={$cfg['db']['host']};port={$cfg['db']['port']};dbname={$cfg['db']['name']};charset=utf8mb4",
                $cfg['db']['user'], $cfg['db']['pass']
            );
            $installed = (int)$pdo->query('SELECT COUNT(*) FROM users')->fetchColumn() > 0;
        } catch (Throwable $e) { $installed = false; }
    }
}

$step = (int)($_POST['step'] ?? $_GET['step'] ?? 1);
$lockFile = $root . '/storage/install.lock';
$msg = ''; $err = '';

if ($installed || is_file($lockFile)) {
    http_response_code(403);
    exit('系统已安装，请删除或限制 install.php 后访问管理平台。');
}

// ========== 工具函数 ==========
function check_result(string $name, bool $ok, string $failMsg = ''): array {
    return ['name' => $name, 'ok' => $ok, 'msg' => $ok ? '' : $failMsg];
}
function test_redis(string $host, int $port, string $auth): string {
    $fp = @fsockopen($host, $port, $errno, $errstr, 3);
    if (!$fp) return "无法连接: $errstr";
    stream_set_timeout($fp, 3);
    $read = function() use ($fp) {
        $line = fgets($fp);
        if ($line === false) return [false, '读取超时'];
        $t = $line[0]; $d = substr($line, 1, -2);
        if ($t === '+') return [true, $d];
        if ($t === '-') return [false, $d];
        if ($t === '$') { $len = (int)$d; return $len < 0 ? [true, null] : [true, fread($fp, $len + 2)]; }
        if ($t === ':') return [true, (int)$d];
        return [true, null];
    };
    if ($auth !== '') {
        fwrite($fp, "*2\r\n$4\r\nAUTH\r\n$" . strlen($auth) . "\r\n$auth\r\n");
        [$ok, $r] = $read($fp);
        if (!$ok) { fclose($fp); return "AUTH 失败: $r"; }
    }
    fwrite($fp, "*1\r\n$4\r\nPING\r\n");
    [$ok, $r] = $read($fp);
    fclose($fp);
    return $ok ? '' : 'PING 失败';
}
function test_mysql(string $host, int $port, string $user, string $pass): string {
    try {
        new PDO("mysql:host=$host;port=$port;charset=utf8mb4", $user, $pass, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION, PDO::ATTR_TIMEOUT => 3]);
        return '';
    } catch (Throwable $e) { return $e->getMessage(); }
}
function run_schema(PDO $pdo, string $db, string $file): string {
    try {
        $sql = file_get_contents($file);
        // 强制使用安装时填写的库名
        $sql = preg_replace('/CREATE DATABASE[^\n]*\n?/i', '', $sql);
        $sql = str_replace('USE dns_manager;', "USE `$db`;", $sql);
        foreach (array_filter(array_map('trim', explode(';', $sql))) as $stmt) {
            if ($stmt === '') continue;
            $pdo->exec($stmt);
        }
        return '';
    } catch (Throwable $e) { return $e->getMessage(); }
}
function write_config(array $cfg, string $file): string {
    $config = [
        'db' => [
            'host' => $cfg['db_host'],
            'port' => (int)$cfg['db_port'],
            'name' => $cfg['db_name'],
            'user' => $cfg['db_user'],
            'pass' => $cfg['db_pass'],
            'charset' => 'utf8mb4',
        ],
        'redis' => [
            'host' => $cfg['redis_host'],
            'port' => (int)$cfg['redis_port'],
            'auth' => $cfg['redis_auth'] ?? '',
            'db' => 0,
            'prefix' => 'dns:',
        ],
        'app' => [
            'secret_key' => bin2hex(random_bytes(32)),
            'cron_key' => bin2hex(random_bytes(32)),
            'captcha_scene' => !empty($cfg['captcha_enable']) ? trim($cfg['captcha_scene'] ?? '') : '',
            'captcha_prefix' => !empty($cfg['captcha_enable']) ? trim($cfg['captcha_prefix'] ?? '') : '',
            'session_ttl' => 86400,
            'max_retry' => 5,
            'retry_base_delay' => 60,
            'timezone' => 'Asia/Shanghai',
        ],
    ];
    $out = "<?php\nreturn " . var_export($config, true) . ";\n";
    if (@file_put_contents($file, $out) === false) return "无法写入 {$file}，请检查目录权限";
    if (function_exists('opcache_invalidate')) @opcache_invalidate($file, true);
    return '';
}

// ========== POST 处理 ==========
$post = $_POST;
if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    if ($step === 2 && isset($post['db_host'], $post['db_port'], $post['db_user'])) {
        $e1 = test_mysql($post['db_host'], (int)$post['db_port'], $post['db_user'], $post['db_pass']);
        $e2 = test_redis($post['redis_host'], (int)$post['redis_port'], $post['redis_auth'] ?? '');
        if ($e1 === '' && $e2 === '') {
            $e3 = write_config($post, $configFile);
            if ($e3 === '') { header('Location: install.php?step=3'); exit; }
            $err = $e3;
        } else {
            $err = trim(($e1 ? "MySQL: $e1" : '') . ' ' . ($e2 ? "Redis: $e2" : ''));
        }
    }
    if ($step === 3) {
        $cfg = require $configFile;
        try {
            $pdo = new PDO(
                "mysql:host={$cfg['db']['host']};port={$cfg['db']['port']};dbname={$cfg['db']['name']};charset=utf8mb4",
                $cfg['db']['user'], $cfg['db']['pass'], [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]
            );
            $e = run_schema($pdo, $cfg['db']['name'], $root . '/database/schema.sql');
            if ($e === '') { header('Location: install.php?step=4'); exit; }
            $err = '导入失败: ' . $e;
        } catch (Throwable $e) { $err = '数据库连接失败: ' . $e->getMessage(); }
    }
    if ($step === 4) {
        $cfg = require $configFile;
        try {
            $pdo = new PDO(
                "mysql:host={$cfg['db']['host']};port={$cfg['db']['port']};dbname={$cfg['db']['name']};charset=utf8mb4",
                $cfg['db']['user'], $cfg['db']['pass'], [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]
            );
            $u = trim($post['username'] ?? ''); $p = $post['password'] ?? '';
            if (strlen($u) < 3 || strlen($p) < 8) $err = '用户名≥3位，密码≥8位';
            else {
                $pdo->prepare('INSERT INTO users (username,password_hash,role) VALUES (?,?,?)')
                    ->execute([$u, password_hash($p, PASSWORD_DEFAULT), 'admin']);
                @mkdir(dirname($lockFile), 0755, true);
                if (@file_put_contents($lockFile, date('c'), LOCK_EX) === false) {
                    throw new RuntimeException('无法创建安装锁，请检查 storage 目录权限');
                }
                header('Location: /'); exit;
            }
        } catch (Throwable $e) { $err = $e->getMessage(); }
    }
}

// ========== 环境检测 ==========
@mkdir($root . '/storage/logs', 0755, true);
$checks = [
    check_result('PHP 版本 ≥ 8.2', version_compare(PHP_VERSION, '8.2.0', '>='), '当前 ' . PHP_VERSION),
    check_result('扩展 pdo_mysql', extension_loaded('pdo_mysql'), '未安装'),
    check_result('扩展 curl', extension_loaded('curl'), '未安装'),
    check_result('扩展 openssl', extension_loaded('openssl'), '未安装（AK/SK 加密需要）'),
    check_result('扩展 mbstring', extension_loaded('mbstring'), '未安装'),
    check_result('扩展 json', extension_loaded('json'), '未安装'),
    check_result('函数 fsockopen 可用', function_exists('fsockopen'), '被禁用（Redis 客户端需要）'),
    check_result('目录 src/config 可写', is_writable(dirname($configFile)), '需要写入配置文件'),
    check_result('目录 storage 可写', is_writable($root . '/storage') || @mkdir($root . '/storage', 0755, true), '需要写入日志'),
];
$allPass = !in_array(false, array_column($checks, 'ok'), true);
?>
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<meta name="color-scheme" content="light">
<title>安装向导 - DNS 统一管理平台</title>
<link rel="stylesheet" href="/assets/install.css">
</head>
<body>
<div class="card">
<?php if ($installed): ?>
    <h1>已完成安装</h1>
    <p class="sub">系统已安装，无需重复安装。</p>
    <div class="actions"><a class="btn primary" href="/">返回首页</a></div>
<?php else: ?>
    <h1>DNS 统一管理平台 - 安装向导</h1>
    <p class="sub">步骤 <?= $step ?> / 5</p>
    <div class="steps"><?php for ($i = 1; $i <= 5; $i++) echo '<span class="' . ($i <= $step ? 'on' : '') . '"></span>'; ?></div>
    <?php if ($err): ?><div class="alert err"><?= htmlspecialchars($err) ?></div><?php endif; ?>

<?php if ($step === 1): ?>
    <table>
    <?php foreach ($checks as $c): ?>
        <tr><td><?= htmlspecialchars($c['name']) ?></td>
            <td class="<?= $c['ok'] ? 'ok' : 'fail' ?>"><?= $c['ok'] ? '通过' : '✗ ' . htmlspecialchars($c['msg']) ?></td></tr>
    <?php endforeach; ?>
    </table>
    <?php if (!$allPass): ?><p class="hint">请先解决以上问题后刷新本页。</p><?php endif; ?>
    <div class="actions"><form method="post"><input type="hidden" name="step" value="2">
        <button class="btn primary" <?= $allPass ? '' : 'disabled' ?>>下一步：配置数据库</button></form></div>

<?php elseif ($step === 2): ?>
    <?php
    $p = $post;
    if (!$p && is_file($configFile)) {
        $c = @require $configFile;
        if (is_array($c)) $p = [
            'db_host' => $c['db']['host'] ?? '', 'db_port' => $c['db']['port'] ?? 3306,
            'db_name' => $c['db']['name'] ?? '', 'db_user' => $c['db']['user'] ?? '',
            'redis_host' => $c['redis']['host'] ?? '', 'redis_port' => $c['redis']['port'] ?? 6379,
        ];
    }
    ?>
    <form method="post">
        <input type="hidden" name="step" value="2">
        <label>MySQL 主机（宝塔环境填 localhost）</label><input name="db_host" value="<?= htmlspecialchars($p['db_host'] ?? 'localhost') ?>" required>
        <label>MySQL 端口</label><input name="db_port" value="<?= htmlspecialchars($p['db_port'] ?? '3306') ?>" required>
        <label>数据库名（请提前在数据库中创建，如 dns_manager）</label><input name="db_name" value="<?= htmlspecialchars($p['db_name'] ?? 'dns_manager') ?>" required>
        <label>MySQL 用户名</label><input name="db_user" value="<?= htmlspecialchars($p['db_user'] ?? 'root') ?>" required>
        <label>MySQL 密码</label><input name="db_pass" type="password" value="">
        <label>Redis 主机</label><input name="redis_host" value="<?= htmlspecialchars($p['redis_host'] ?? '127.0.0.1') ?>" required>
        <label>Redis 端口</label><input name="redis_port" value="<?= htmlspecialchars($p['redis_port'] ?? '6379') ?>" required>
        <label>Redis 密码（无则留空）</label><input name="redis_auth" type="password" value="">
        <label class="captcha-toggle"><input type="checkbox" name="captcha_enable" id="captcha_enable" value="1">
            启用阿里云 ESA 安全验证码（<a href="https://esa.console.aliyun.com/captcha/configuration" target="_blank" rel="noopener">前往控制台配置</a>）</label>
        <div id="captcha-fields" style="display:none">
            <label>ESA Scene ID（场景 ID）</label><input name="captcha_scene" value="<?= htmlspecialchars($p['captcha_scene'] ?? '') ?>" placeholder="如 1iiz4exb">
            <label>ESA Prefix（验证码前缀）</label><input name="captcha_prefix" value="<?= htmlspecialchars($p['captcha_prefix'] ?? '') ?>" placeholder="如 esa-xxxxxxxxxx">
            <p class="hint">在 ESA 验证码配置的「需验签的接口（URI）」中填写：<code>你的域名/*</code> 即可。</p>
        </div>
        <p class="hint">提交时将自动测试 MySQL / Redis 连接，并把配置写入 src/config/config.php（secret_key 自动生成）。不启用验证码时登录页不做安全校验。</p>
        <div class="actions"><a class="btn" href="install.php?step=1">上一步</a>
            <button class="btn primary">测试连接并保存配置</button></div>
    </form>
    <script>
    (function () {
        var cb = document.getElementById('captcha_enable'), f = document.getElementById('captcha-fields');
        var sync = function () { f.style.display = cb.checked ? 'block' : 'none'; };
        cb.addEventListener('change', sync); sync();
    })();
    </script>

<?php elseif ($step === 3 && is_file($configFile)): ?>
    <?php $cfg = @require $configFile; ?>
    <p>将使用以下配置导入 <code>database/schema.sql</code>（会先删除旧表）：</p>
    <table>
        <tr><td>主机</td><td><?= htmlspecialchars($cfg['db']['host'] ?? '-') ?></td></tr>
        <tr><td>数据库</td><td><b><?= htmlspecialchars($cfg['db']['name'] ?? '-') ?></b></td></tr>
        <tr><td>用户</td><td><?= htmlspecialchars($cfg['db']['user'] ?? '-') ?></td></tr>
    </table>
    <p class="hint">如果上面显示的不是你要的数据库，请点「上一步」重新填写并提交（密码框每次都需要重新输入）。</p>
    <form method="post" style="margin-top:18px">
        <input type="hidden" name="step" value="3">
        <div class="actions"><a class="btn" href="install.php?step=2&force=1">上一步</a>
            <button class="btn primary">开始导入</button></div>
    </form>

<?php elseif ($step === 4): ?>
    <form method="post">
        <input type="hidden" name="step" value="4">
        <label>管理员用户名</label><input name="username" value="<?= htmlspecialchars($post['username'] ?? 'admin') ?>" required>
        <label>管理员密码（≥8位）</label><input name="password" type="password" required>
        <div class="actions"><a class="btn" href="install.php?step=3">上一步</a>
            <button class="btn primary">创建管理员</button></div>
    </form>

<?php endif; ?>
<?php endif; ?>
<p class="author-line">开源项目 <a href="https://github.com/FQkun/all-in-dns" target="_blank" rel="noopener">FQkun/all-in-dns</a></p>
</div>
</body>
</html>
