<?php
// 宝塔计划任务（访问URL）触发入口，每 1 分钟访问一次：
//   /cron.php?key=<cron_key>
declare(strict_types=1);
require __DIR__ . '/../src/bootstrap.php';

use App\Core\Database;
use App\Core\SyncService;

$key = (string)($_SERVER['HTTP_X_CRON_KEY'] ?? '');
$cronKey = (string)($GLOBALS['config']['app']['cron_key'] ?? '');
if ($key === '' || $cronKey === '' || !hash_equals($cronKey, $key)) {
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

// 清理孤儿数据：记录/域名已删除后残留的队列与映射，避免向云端重放
$orphanQueue = (int)$db->exec('DELETE q FROM sync_queue q LEFT JOIN records r ON r.id=q.record_id WHERE r.id IS NULL');
$orphanMap = (int)$db->exec('DELETE m FROM record_provider_map m LEFT JOIN records r ON r.id=m.record_id WHERE r.id IS NULL');
if ($orphanQueue || $orphanMap) echo "清理孤儿数据: 队列 $orphanQueue 条 / 映射 $orphanMap 条\n";

// 每天首个执行：自动备份（保留最近30份）
$today = date('Y-m-d');
$st = $db->prepare("SELECT COUNT(*) c FROM backups WHERE created_by='system' AND name=?");
$st->execute(['自动备份 ' . $today]);
if ((int)$st->fetch()['c'] === 0) {
    $data = json_encode([
        'schema_version' => 2,
        'created_at' => date('Y-m-d H:i:s'),
        'domains' => $db->query('SELECT * FROM domains')->fetchAll(),
        'domain_providers' => $db->query('SELECT * FROM domain_providers')->fetchAll(),
        'records' => $db->query('SELECT * FROM records')->fetchAll(),
        'record_provider_map' => $db->query('SELECT * FROM record_provider_map')->fetchAll(),
    ], JSON_UNESCAPED_UNICODE);
    $db->prepare('INSERT INTO backups (name,data,record_count,created_by,is_gzip) VALUES (?,?,?,?,1)')
        ->execute(['自动备份 ' . $today, gzencode((string)$data, 6),
                   (int)$db->query('SELECT COUNT(*) c FROM records')->fetch()['c'], 'system']);
    $db->exec('DELETE FROM backups WHERE created_by=\'system\' AND id NOT IN
        (SELECT id FROM (SELECT id FROM backups WHERE created_by=\'system\' ORDER BY id DESC LIMIT 30) t)');
    echo "自动备份完成\n";
}
