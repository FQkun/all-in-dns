<?php
// 定时任务：失败队列重试 + 异常告警日志
// crontab: * * * * * php /var/www/dns_all/cron/retry.php >> /var/log/dns_retry.log 2>&1
declare(strict_types=1);
require dirname(__DIR__) . '/src/bootstrap.php';

use App\Core\Database;
use App\Core\SyncService;

$svc = new SyncService();
$done = $svc->processPendingQueue(100);
$ok = count(array_filter($done, fn($d) => $d['status'] === 'success'));
$fail = count($done) - $ok;
echo '[' . date('Y-m-d H:i:s') . "] 重试: 共 " . count($done) . " 成功 $ok 失败 $fail\n";

// 监控告警：连续失败任务数超阈值写入系统日志（可对接邮件/webhook）
$failCount = (int)Database::i()->query("SELECT COUNT(*) c FROM sync_queue WHERE status='failed'")->fetch()['c'];
if ($failCount >= 10) {
    error_log("[DNS-MONITOR][ALERT] $failCount 个同步任务最终失败，请检查云账号配置");
}
