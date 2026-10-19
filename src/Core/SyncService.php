<?php
namespace App\Core;

use App\Core\Providers\BaseProvider;
use App\Core\Providers\ProviderFactory;
use App\Core\Providers\ProviderException;

class SyncService
{
    private \PDO $db;

    public function __construct()
    {
        $this->db = Database::i();
    }

    /** 将一条解析记录的变更扇出到所有启用同步的云厂商 */
    public function dispatch(int $recordId, string $action): array
    {
        $rec = $this->getRecord($recordId);
        if (!$rec) throw new \RuntimeException('记录不存在');

        $providers = $this->db->prepare(
            'SELECT dp.account_id, dp.provider, ca.provider AS p2 FROM domain_providers dp
             JOIN cloud_accounts ca ON ca.id = dp.account_id AND ca.status = 1
             WHERE dp.domain_id = ? AND dp.sync_enabled = 1');
        $providers->execute([$rec['domain_id']]);
        $accounts = $providers->fetchAll();

        $queueIds = [];
        $st = $this->db->prepare('INSERT INTO sync_queue (record_id,account_id,action,payload) VALUES (?,?,?,?)');
        foreach ($accounts as $acc) {
            $st->execute([$recordId, $acc['account_id'], $action, json_encode([
                'domain' => $rec['domain'],
                'record' => $this->normalizeRecord($rec),
            ], JSON_UNESCAPED_UNICODE)]);
            $queueIds[] = (int)$this->db->lastInsertId();
        }
        return $this->executeBatch($queueIds);
    }

    /** 批量扇出：多条记录的同一动作一次性入队后并行执行 */
    public function dispatchMany(array $recordIds, string $action): array
    {
        $queueIds = [];
        $ins = $this->db->prepare('INSERT INTO sync_queue (record_id,account_id,action,payload) VALUES (?,?,?,?)');
        $provSt = $this->db->prepare(
            'SELECT dp.account_id FROM domain_providers dp
             JOIN cloud_accounts ca ON ca.id = dp.account_id AND ca.status = 1
             WHERE dp.domain_id = ? AND dp.sync_enabled = 1');
        foreach ($recordIds as $rid) {
            $rec = $this->getRecord((int)$rid);
            if (!$rec) continue;
            $provSt->execute([(int)$rec['domain_id']]);
            foreach ($provSt->fetchAll() as $acc) {
                $ins->execute([(int)$rid, (int)$acc['account_id'], $action, json_encode([
                    'domain' => $rec['domain'],
                    'record' => $this->normalizeRecord($rec),
                ], JSON_UNESCAPED_UNICODE)]);
                $queueIds[] = (int)$this->db->lastInsertId();
            }
        }
        return $this->executeBatch($queueIds);
    }

    private function normalizeRecord(array $rec): array
    {
        return [
            'sub' => $rec['sub_domain'] ?: '@',
            'type' => $rec['type'],
            'line' => $rec['line'] ?: '默认',
            'value' => $rec['value'],
            'ttl' => (int)$rec['ttl'],
            'priority' => $rec['priority'] !== null ? (int)$rec['priority'] : null,
        ];
    }

    private function getRecord(int $recordId): ?array
    {
        $st = $this->db->prepare(
            'SELECT r.*, d.name AS domain FROM records r JOIN domains d ON d.id = r.domain_id WHERE r.id = ?');
        $st->execute([$recordId]);
        return $st->fetch() ?: null;
    }

    private function markMap(int $recordId, int $accountId, string $provider, string $status, string $msg = '', ?string $providerRecordId = null): void
    {
        $this->db->prepare(
            'INSERT INTO record_provider_map (record_id,account_id,provider,provider_record_id,sync_status,sync_message,synced_at,retry_count)
             VALUES (?,?,?,?,?,?,NOW(),0)
             ON DUPLICATE KEY UPDATE sync_status=VALUES(sync_status), sync_message=VALUES(sync_message),
             synced_at=IF(VALUES(sync_status)=\'success\' OR VALUES(sync_status)=\'deleted\', NOW(), synced_at),
             provider_record_id=IF(VALUES(provider_record_id) IS NOT NULL, VALUES(provider_record_id), provider_record_id)')
            ->execute([$recordId, $accountId, $provider, $providerRecordId, $status, mb_substr($msg, 0, 500)]);
    }

    /** 批量并行执行队列任务；单任务失败按指数退避安排重试，不影响其他任务 */
    private function executeBatch(array $jobIds): array
    {
        $jobIds = array_map('intval', array_values($jobIds));
        if (!$jobIds) return [];
        $in = implode(',', $jobIds);

        $st = $this->db->query(
            "SELECT q.*, ca.provider FROM sync_queue q
             JOIN cloud_accounts ca ON ca.id = q.account_id WHERE q.id IN ($in)");
        $jobs = [];
        foreach ($st->fetchAll() as $row) $jobs[(int)$row['id']] = $row;
        foreach (array_keys($jobs) as $queueId) {
            $claim = $this->db->prepare("UPDATE sync_queue SET status='processing' WHERE id=? AND status='pending'");
            $claim->execute([$queueId]);
            if ($claim->rowCount() === 0) unset($jobs[$queueId]);
        }

        $maxRetry = $GLOBALS['config']['app']['max_retry'];
        $results = [];
        $tasks = [];
        $taskJobs = [];

        $aliveSt = $this->db->prepare('SELECT COUNT(*) FROM records r JOIN domains d ON d.id=r.domain_id WHERE r.id=?');
        foreach ($jobs as $queueId => $job) {
            if ($job['action'] === 'delete') continue;
            $aliveSt->execute([(int)$job['record_id']]);
            if ((int)$aliveSt->fetchColumn() === 0) {
                $this->db->prepare("UPDATE sync_queue SET status='success', executed_at=NOW(), last_error='记录已删除，跳过同步' WHERE id=?")
                    ->execute([$queueId]);
                $results[$queueId] = ['queue_id' => $queueId, 'provider' => $job['provider'], 'status' => 'success', 'message' => '记录已删除，跳过'];
                unset($jobs[$queueId]);
            }
        }

        foreach ($jobIds as $queueId) {
            if (!isset($jobs[$queueId])) {
                if (!isset($results[$queueId])) $results[$queueId] = ['queue_id' => $queueId, 'status' => 'failed', 'message' => '任务不存在'];
                continue;
            }
            $job = $jobs[$queueId];
            $payload = json_decode($job['payload'], true);
            $recordId = (int)$job['record_id'];
            $accountId = (int)$job['account_id'];
            try {
                $prov = ProviderFactory::makeById($accountId);
                $remoteId = $this->getRemoteId($recordId, $accountId);
                if ($job['action'] === 'update' && $remoteId === null) {
                    $remoteId = $this->alignExisting($accountId, $payload);
                }
                if ($job['action'] === 'delete' && $remoteId === null) {
                    $found = $this->alignExisting($accountId, $payload);
                    if ($found === null) {
                        $this->markMap($recordId, $accountId, $job['provider'], 'deleted', '', null);
                        $this->db->prepare('UPDATE record_provider_map SET provider_record_id=NULL WHERE record_id=? AND account_id=?')
                            ->execute([$recordId, $accountId]);
                        $this->db->prepare('UPDATE sync_queue SET status=\'success\', executed_at=NOW(), last_error=NULL WHERE id=?')->execute([$queueId]);
                        Logger::sync($recordId, $accountId, $job['provider'], 'delete', true, '远端不存在，视为已删除');
                        $results[$queueId] = ['queue_id' => $queueId, 'provider' => $job['provider'], 'status' => 'success'];
                        continue;
                    }
                    $remoteId = $found;
                }
                [$action, $params] = $prov->opParams($job['action'], $payload['domain'], $payload['record'], $remoteId);
                $taskJobs[count($tasks)] = ['queue_id' => $queueId, 'remote_id' => $remoteId];
                $tasks[] = ['provider' => $prov, 'action' => $action, 'params' => $params];
            } catch (\Throwable $e) {
                $results[$queueId] = $this->failJob($job, $e->getMessage(), $maxRetry);
            }
        }

        foreach (BaseProvider::callParallel($tasks) as $i => $out) {
            $meta = $taskJobs[$i];
            $queueId = $meta['queue_id'];
            $job = $jobs[$queueId];
            $recordId = (int)$job['record_id'];
            $accountId = (int)$job['account_id'];
            $costMs = isset($out['_cost_ms']) ? (int)$out['_cost_ms'] : null;
            if (isset($out['error'])) {
                $err = (string)$out['error'];
                if ($this->isDuplicateError($err) && $job['action'] !== 'delete') {
                    $payload = json_decode($job['payload'], true);
                    $remoteId = $this->alignExisting($accountId, $payload);
                    if ($remoteId !== null) {
                        $this->markMap($recordId, $accountId, $job['provider'], 'success', '', $remoteId);
                        $this->db->prepare('UPDATE sync_queue SET status=\'success\', executed_at=NOW(), last_error=NULL WHERE id=?')->execute([$queueId]);
                        Logger::sync($recordId, $accountId, $job['provider'], $job['action'], true, '远端已存在，已自动对齐', $costMs);
                        $results[$queueId] = ['queue_id' => $queueId, 'provider' => $job['provider'], 'status' => 'success'];
                        continue;
                    }
                }
                $results[$queueId] = $this->failJob($job, $err, $maxRetry, $costMs);
                continue;
            }
            if ($job['action'] === 'delete') $newId = null;
            elseif ($meta['remote_id'] !== null) $newId = (string)$meta['remote_id'];
            else $newId = (string)($out['RecordId'] ?? $out['RecordID'] ?? '');
            $this->markMap($recordId, $accountId, $job['provider'], $job['action'] === 'delete' ? 'deleted' : 'success', '', $newId);
            $this->db->prepare('UPDATE sync_queue SET status=\'success\', executed_at=NOW(), last_error=NULL WHERE id=?')->execute([$queueId]);
            Logger::sync($recordId, $accountId, $job['provider'], $job['action'], true, '', $costMs);
            $results[$queueId] = ['queue_id' => $queueId, 'provider' => $job['provider'], 'status' => 'success'];
        }

        $ordered = [];
        foreach ($jobIds as $queueId) {
            $ordered[] = $results[$queueId] ?? ['queue_id' => $queueId, 'status' => 'failed', 'message' => '任务不存在'];
        }
        return $ordered;
    }

    /** 各厂商“远端已存在同记录”类错误（SubdomainRollLimit：同子域名已有记录时重复创建也会触发） */
    private function isDuplicateError(string $msg): bool
    {
        return str_contains($msg, 'ErrDBDuplicated')
            || str_contains($msg, 'DomainRecordDuplicate')
            || str_contains($msg, 'ResourceAlreadyExists')
            || str_contains($msg, 'SubdomainRollLimit')
            || str_contains($msg, 'already have duplicate record');
    }

    /** 在远端按归一化键查找已存在的同记录，返回其远端 ID；找不到返回 null */
    private function alignExisting(int $accountId, array $payload): ?string
    {
        try {
            $rec = $payload['record'] ?? null;
            if (!$rec) return null;
            $want = self::recKey((string)$rec['sub'], (string)$rec['type'], (string)$rec['value'], (string)($rec['line'] ?? '默认'));
            $prov = ProviderFactory::makeById($accountId);
            foreach ($prov->listRecords((string)$payload['domain']) as $rr) {
                if (self::recKey((string)$rr['sub'], (string)$rr['type'], (string)$rr['value'], (string)($rr['line'] ?? '默认')) === $want) {
                    return (string)$rr['id'];
                }
            }
        } catch (\Throwable $e) {
            return null;
        }
        return null;
    }

    private function failJob(array $job, string $message, int $maxRetry, ?int $costMs = null): array
    {
        $queueId = (int)$job['id'];
        $recordId = (int)$job['record_id'];
        $accountId = (int)$job['account_id'];
        $retry = (int)$job['retry_count'] + 1;
        $failed = $retry >= $maxRetry;
        $delay = $GLOBALS['config']['app']['retry_base_delay'] * (2 ** ($retry - 1));
        $this->db->prepare('UPDATE sync_queue SET status=?, retry_count=?, last_error=?, next_retry_at=DATE_ADD(NOW(), INTERVAL ? SECOND) WHERE id=?')
            ->execute([$failed ? 'failed' : 'pending', $retry, mb_substr($message, 0, 500), $delay, $queueId]);
        $this->markMap($recordId, $accountId, $job['provider'], 'failed', $message);
        Logger::sync($recordId, $accountId, $job['provider'], $job['action'], false, $message, $costMs);
        return ['queue_id' => $queueId, 'provider' => $job['provider'], 'status' => 'failed', 'message' => $message, 'will_retry' => !$failed];
    }

    private function getRemoteId(int $recordId, int $accountId): ?string
    {
        $st = $this->db->prepare('SELECT provider_record_id FROM record_provider_map WHERE record_id=? AND account_id=?');
        $st->execute([$recordId, $accountId]);
        $row = $st->fetch();
        $id = $row['provider_record_id'] ?? null;
        return ($id !== null && $id !== '') ? $id : null;
    }

    /** cron 调用：重试待处理任务 */
    public function processPendingQueue(int $limit = 50): array
    {
        $this->db->exec("UPDATE sync_queue SET status='pending' WHERE status='processing' AND executed_at IS NULL AND created_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE)");
        $st = $this->db->prepare(
            "SELECT q.id, ca.provider FROM sync_queue q
             JOIN cloud_accounts ca ON ca.id = q.account_id
             WHERE q.status = 'pending' AND (q.next_retry_at IS NULL OR q.next_retry_at <= NOW())
             ORDER BY q.id ASC LIMIT " . (int)$limit);
        $st->execute();
        $queueIds = [];
        foreach ($st->fetchAll() as $job) {
            $queueIds[] = (int)$job['id'];
        }
        return $this->executeBatch($queueIds);
    }

    /** 数据一致性校验：对比本地记录与各厂商远端记录 */
    public function verifyDomain(int $domainId): array
    {
        $st = $this->db->prepare('SELECT * FROM domains WHERE id=?');
        $st->execute([$domainId]);
        $domain = $st->fetch();
        if (!$domain) throw new \RuntimeException('域名不存在');

        $local = $this->db->prepare("SELECT id, sub_domain, type, line, value FROM records WHERE domain_id=? AND status != 'disabled'");
        $local->execute([$domainId]);
        $localKeys = [];
        foreach ($local->fetchAll() as $r) {
            if (strtoupper($r['type']) === 'NS' && strtolower($r['sub_domain']) === '@') continue;
            $localKeys[self::recKey($r['sub_domain'], $r['type'], $r['value'], (string)$r['line'])] = (int)$r['id'];
        }

        $dp = $this->db->prepare('SELECT * FROM domain_providers WHERE domain_id=? AND sync_enabled=1');
        $dp->execute([$domainId]);
        $report = [];
        foreach ($dp->fetchAll() as $dpRow) {
            $aid = (int)$dpRow['account_id'];
            try {
                $prov = ProviderFactory::makeById($aid);
                $remote = $prov->listRecords($domain['name']);
                $remoteKeys = [];
                foreach ($remote as $rr) {
                    if (strtoupper((string)$rr['type']) === 'NS' && strtolower((string)$rr['sub']) === '@') continue;
                    $remoteKeys[self::recKey($rr['sub'], $rr['type'], $rr['value'], (string)($rr['line'] ?? '默认'))] = $rr['id'];
                }
                $missing = array_diff_key($localKeys, $remoteKeys);
                $extra = array_diff_key($remoteKeys, $localKeys);
                $missingList = [];
                foreach ($missing as $k => $rid) $missingList[] = ['id' => (int)$rid, 'key' => $k];
                $report[] = [
                    'account_id' => $aid, 'provider' => $dpRow['provider'],
                    'status' => (empty($missing) && empty($extra)) ? 'consistent' : 'inconsistent',
                    'local_count' => count($localKeys), 'remote_count' => count($remoteKeys),
                    'missing_local' => count($missing), 'extra_remote' => count($extra),
                    'detail' => ['missing' => $missingList, 'extra' => array_keys($extra)],
                ];
                $ok = empty($missing) && empty($extra) ? 'ok' : 'fail';
                $this->db->prepare('UPDATE domain_providers SET last_verify_at=NOW(), verify_status=? WHERE domain_id=? AND account_id=?')
                    ->execute([$ok, $domainId, $aid]);
            } catch (\Throwable $e) {
                $report[] = ['account_id' => $aid, 'provider' => $dpRow['provider'], 'status' => 'error',
                    'local_count' => count($localKeys), 'remote_count' => 0, 'message' => $e->getMessage()];
            }
        }
        if (!$report) $report[] = ['provider' => '-', 'status' => 'error', 'message' => '该域名未绑定任何启用同步的云平台'];
        return $report;
    }

    /** 把指定记录仅推送到指定云账号（一致性补齐用）；先清可能失效的远端映射，统一走创建 */
    public function pushToAccount(int $domainId, int $accountId, array $recordIds): array
    {
        $st = $this->db->prepare('SELECT name FROM domains WHERE id=?');
        $st->execute([$domainId]);
        $domain = $st->fetch();
        if (!$domain) throw new \RuntimeException('域名不存在');

        $recSt = $this->db->prepare('SELECT * FROM records WHERE id=? AND domain_id=?');
        $delMap = $this->db->prepare('DELETE FROM record_provider_map WHERE record_id=? AND account_id=?');
        $ins = $this->db->prepare('INSERT INTO sync_queue (record_id,account_id,action,payload) VALUES (?,?,?,?)');
        $queueIds = [];
        foreach ($recordIds as $rid) {
            $recSt->execute([(int)$rid, $domainId]);
            $rec = $recSt->fetch();
            if (!$rec) continue;
            $delMap->execute([(int)$rid, $accountId]);
            $ins->execute([(int)$rid, $accountId, 'create', json_encode([
                'domain' => $domain['name'],
                'record' => $this->normalizeRecord($rec),
            ], JSON_UNESCAPED_UNICODE)]);
            $queueIds[] = (int)$this->db->lastInsertId();
        }
        return $this->executeBatch($queueIds);
    }

    /** 归一化线路名：把各平台"默认线路"写法统一 */
    public static function normLine(?string $line): string
    {
        $l = trim((string)$line);
        if ($l === '' || $l === '默认' || strcasecmp($l, 'default') === 0) return '默认';
        return $l;
    }

    /** 归一化记录键：跨厂商合并用（含解析线路；主机大小写、类型大写、主机名类值去尾点/小写） */
    public static function recKey(string $sub, string $type, string $value, ?string $line = '默认'): string
    {
        $t = strtoupper(trim($type));
        $v = trim($value);
        if ($t === 'MX') $v = preg_replace('/^\d+\s+/', '', $v);
        if (in_array($t, ['CNAME', 'MX', 'NS', 'SRV'], true)) $v = rtrim($v, '.');
        if (in_array($t, ['CNAME', 'MX', 'NS', 'SRV', 'A', 'AAAA'], true)) $v = strtolower($v);
        return strtolower(trim($sub)) . '|' . $t . '|' . $v . '|' . self::normLine($line);
    }

    /** 合并同一域名下的重复记录：按归一化键分组，保留最早一条，其余的云映射转移后删除 */
    public function mergeDuplicates(int $domainId): int
    {
        $st = $this->db->prepare('SELECT id, sub_domain, type, line, value FROM records WHERE domain_id=? ORDER BY id');
        $st->execute([$domainId]);
        $groups = [];
        foreach ($st->fetchAll() as $r) {
            $groups[self::recKey($r['sub_domain'], $r['type'], $r['value'], (string)$r['line'])][] = (int)$r['id'];
        }
        $removed = 0;
        foreach ($groups as $ids) {
            if (count($ids) < 2) continue;
            $keep = array_shift($ids);
            foreach ($ids as $dup) {
                // 同账号映射冲突的先删除，避免主键冲突
                $this->db->prepare(
                    'DELETE m FROM record_provider_map m
                     JOIN record_provider_map k ON k.record_id=? AND k.account_id=m.account_id
                     WHERE m.record_id=?')->execute([$keep, $dup]);
                $this->db->prepare('UPDATE record_provider_map SET record_id=? WHERE record_id=?')
                    ->execute([$keep, $dup]);
                $this->db->prepare('DELETE FROM records WHERE id=?')->execute([$dup]);
                $removed++;
            }
        }
        return $removed;
    }
}
