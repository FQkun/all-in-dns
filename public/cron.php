<?php
// 宝塔计划任务（访问URL）触发入口，每 1 分钟访问一次：
//   /cron.php?key=<cron_key>
declare(strict_types=1);
require __DIR__ . '/../src/bootstrap.php';

use App\Core\Database;
use App\Core\SyncService;

$key = $_SERVER['HTTP_X_CRON_KEY'] ?? ($_GET['key'] ?? '');
$cronKey = (string)($GLOBALS['config']['app']['cron_key'] ?? '');
if ($cronKey === '' || !hash_equals($cronKey, $key)) {
    http_response_code(403);
    exit('forbidden');
}

header('Content-Type: text/plain; charset=utf-8');
$db = Database::i();

// 每次执行：失败队列重试
$done = (new SyncService())->processPendingQueue(100);
$ok = count(array_filter($done, fn($d) => $d['status'] === 'success'));
echo '[' . date('Y-m-d H:i:s') . "] 重试: 共 " . count($done) . " 成功 $ok 失败 " . (count($done) - $ok) . "\n";

$failCount = (int)$db->query("SELECT COUNT(*) c FROM sync_queue WHERE status='failed'")->fetch()['c'];
if ($failCount >= 10) echo "[ALERT] $failCount 个同步任务最终失败，请检查云账号配置\n";

// 每天首个执行：自动备份（保留最近30份）
$today = date('Y-m-d');
$st = $db->prepare("SELECT COUNT(*) c FROM backups WHERE created_by='system' AND name=?");
$st->execute(['自动备份 ' . $today]);
if ((int)$st->fetch()['c'] === 0) {
    $data = json_encode([
        'created_at' => date('Y-m-d H:i:s'),
        'domains' => $db->query('SELECT * FROM domains')->fetchAll(),
        'domain_providers' => $db->query('SELECT * FROM domain_providers')->fetchAll(),
        'records' => $db->query('SELECT * FROM records')->fetchAll(),
        'record_provider_map' => $db->query('SELECT * FROM record_provider_map')->fetchAll(),
    ], JSON_UNESCAPED_UNICODE);
    $db->prepare('INSERT INTO backups (name,data,record_count,created_by) VALUES (?,?,?,?)')
        ->execute(['自动备份 ' . $today, $data,
                   (int)$db->query('SELECT COUNT(*) c FROM records')->fetch()['c'], 'system']);
    $db->exec('DELETE FROM backups WHERE created_by=\'system\' AND id NOT IN
        (SELECT id FROM (SELECT id FROM backups WHERE created_by=\'system\' ORDER BY id DESC LIMIT 30) t)');
    echo "自动备份完成\n";
}
