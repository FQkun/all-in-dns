<?php
// 定时任务：每日自动备份 records 快照
// crontab: 0 3 * * * php /var/www/dns_all/cron/backup.php
declare(strict_types=1);
if (PHP_SAPI !== 'cli') { http_response_code(403); exit('cli only'); }
require dirname(__DIR__) . '/src/bootstrap.php';

use App\Core\Database;

$db = Database::i();
$data = json_encode([
    'schema_version' => 2,
    'created_at' => date('Y-m-d H:i:s'),
    'domains' => $db->query('SELECT * FROM domains')->fetchAll(),
    'domain_providers' => $db->query('SELECT * FROM domain_providers')->fetchAll(),
    'records' => $db->query('SELECT * FROM records')->fetchAll(),
], JSON_UNESCAPED_UNICODE);

$db->prepare('INSERT INTO backups (name,data,record_count,created_by,is_gzip) VALUES (?,?,?,?,1)')
    ->execute(['自动备份 ' . date('Y-m-d'), gzencode((string)$data, 6),
               (int)$db->query('SELECT COUNT(*) c FROM records')->fetch()['c'], 'system']);

// 只保留最近 30 个自动备份
$db->exec('DELETE FROM backups WHERE created_by=\'system\' AND id NOT IN
    (SELECT id FROM (SELECT id FROM backups WHERE created_by=\'system\' ORDER BY id DESC LIMIT 30) t)');
echo '[' . date('Y-m-d H:i:s') . "] 自动备份完成\n";
