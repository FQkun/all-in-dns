<?php
// API 路由表：注册所有接口
use App\Core\Auth;
use App\Core\Crypto;
use App\Core\Database;
use App\Core\Http;
use App\Core\Logger;
use App\Core\RedisClient;
use App\Core\Providers\ProviderFactory;
use App\Core\SyncService;

return function (App\Core\Router $r) {
    $validateRecordConflict = function (PDO $db, int $domainId, string $sub, string $type, string $line, string $value = '', ?int $excludeId = null): void {
        $type = strtoupper(trim($type));
        $sub = strtolower(trim($sub ?: '@'));
        $line = trim($line ?: '默认');
        $notSelf = $excludeId !== null ? ' AND id<>?' : '';

        if ($value !== '') {
            $dupArgs = [$domainId, $sub, $type, $line, $value];
            if ($excludeId !== null) $dupArgs[] = $excludeId;
            $dup = $db->prepare("SELECT status FROM records WHERE domain_id=? AND LOWER(sub_domain)=? AND type=? AND line=? AND value=?$notSelf");
            $dup->execute($dupArgs);
            $dupStatus = $dup->fetchColumn();
            if ($dupStatus !== false) {
                Http::fail($dupStatus === 'disabled'
                    ? '记录已存在（该记录待云端清理，请先在列表重试删除）'
                    : '记录已存在：同一主机 / 线路 / 类型下已有相同记录值', 409);
            }
        }

        $conflictTypes = in_array($type, ['A', 'AAAA'], true) ? ['CNAME']
            : ($type === 'CNAME' ? ['A', 'AAAA'] : []);
        if (!$conflictTypes) return;
        $placeholders = implode(',', array_fill(0, count($conflictTypes), '?'));
        $args = array_merge([$domainId, $sub, $line], $conflictTypes);
        if ($excludeId !== null) $args[] = $excludeId;
        $st = $db->prepare(
            "SELECT type FROM records WHERE domain_id=? AND LOWER(sub_domain)=? AND line=? AND status='active' AND type IN ($placeholders)$notSelf");
        $st->execute($args);
        $found = $st->fetchColumn();
        if ($found !== false) Http::fail("记录冲突：{$sub} / {$line} 已存在 {$found} 记录，不能与 {$type} 共存", 409);
    };

    $checkRecordInput = function (array $in): array {
        $types = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA'];
        $type = strtoupper(trim((string)($in['type'] ?? '')));
        $sub = trim((string)($in['sub'] ?? '@')) ?: '@';
        $line = trim((string)($in['line'] ?? '默认')) ?: '默认';
        $value = trim((string)($in['value'] ?? ''));
        $ttl = (int)($in['ttl'] ?? 600);
        $err = '';

        if (!in_array($type, $types, true)) $err = '不支持的记录类型';
        elseif ($value === '') $err = '记录值不能为空';
        elseif (mb_strlen($value) > 1024) $err = '记录值过长';
        elseif (mb_strlen($sub) > 253 || mb_strlen($line) > 64) $err = '主机记录或线路过长';
        elseif ($ttl < 60 || $ttl > 86400) $err = 'TTL 需在 60~86400 秒之间';

        $patterns = [
            'A' => '/^(\d{1,3}\.){3}\d{1,3}$/',
            'AAAA' => '/^[0-9a-fA-F:]{2,39}$/',
            'CNAME' => '/^([a-z0-9_*.-]+\.)+[a-z]{2,}\.?$/i',
        ];
        if ($err === '' && isset($patterns[$type]) && !preg_match($patterns[$type], $value)) $err = '记录值格式不正确';

        $rawPriority = $in['priority'] ?? null;
        $priority = ($rawPriority === null || $rawPriority === '') ? null : (int)$rawPriority;
        if ($err === '' && $type === 'MX' && ($priority === null || $priority < 0 || $priority > 65535)) $err = 'MX 优先级不合法';
        if ($type !== 'MX') $priority = null;

        return [[$sub, $type, $line, $value, $ttl, $priority], $err];
    };

    $validateRecordInput = function (array $in) use ($checkRecordInput): array {
        [$data, $err] = $checkRecordInput($in);
        if ($err !== '') Http::fail($err);
        return $data;
    };

    // ---------- 认证 ----------
    $r->post('/api/auth/login', function () {
        $in = Http::input();
        $captchaOn = trim((string)($GLOBALS['config']['app']['captcha_prefix'] ?? '')) !== '';
        $captchaParam = trim((string)($in['captcha'] ?? ($_SERVER['HTTP_CAPTCHA_VERIFY_PARAM'] ?? '')));
        if ($captchaOn && $captchaParam === '') Http::fail('请先完成安全验证', 403);

        $username = (string)($in['username'] ?? '');
        $ip = Http::ip();
        $limitKey = 'loginfail:' . $ip . ':' . strtolower($username);
        $fails = 0;
        try {
            $fails = (int)(RedisClient::i()->get($limitKey) ?? 0);
        } catch (Throwable $e) {
            $fails = 0;
        }
        if ($fails >= 10) Http::fail('尝试次数过多，请 10 分钟后再试', 429);

        $u = Auth::attempt($username, (string)($in['password'] ?? ''));
        if (!$u) {
            try { RedisClient::i()->incr($limitKey, 600); } catch (Throwable $e) {}
            Logger::op(null, $username, 'login.failed', '', ['ip' => $ip]);
            Http::fail('用户名或密码错误', 401);
        }
        try { RedisClient::i()->del($limitKey); } catch (Throwable $e) {}
        Auth::login($u);
        Logger::op((int)$u['id'], $u['username'], 'login');
        Http::ok(['user' => $u]);
    });
    $r->post('/api/auth/logout', fn() => (Auth::logout() ?: Http::ok()));
    $r->get('/api/auth/me', fn() => Http::ok(['user' => Auth::requireUser()]));

    // ---------- 云账号 ----------
    $r->get('/api/accounts', function () {
        Auth::requireUser();
        $rows = Database::i()->query('SELECT id,provider,name,status,created_at FROM cloud_accounts ORDER BY id')->fetchAll();
        foreach ($rows as &$row) $row['access_key_id_masked'] = '****';
        Http::ok($rows);
    });
    $r->post('/api/accounts', function () {
        $u = Auth::requireRole('admin');
        $in = Http::input();
        if (!in_array($in['provider'] ?? '', ['tencent', 'aliyun', 'volc'], true)) Http::fail('无效的云厂商');
        if (!isset($in['access_key_id'], $in['access_key_secret']) || $in['access_key_id'] === '' || $in['access_key_secret'] === '') Http::fail('AK/SK 不能为空');
        $extra = array_intersect_key((array)($in['extra'] ?? []), array_flip(['session_token', 'region']));
        if (!empty($extra['session_token'])) $extra['session_token'] = Crypto::encrypt((string)$extra['session_token']);
        Database::i()->prepare('INSERT INTO cloud_accounts (provider,name,access_key_id,access_key_secret,extra) VALUES (?,?,?,?,?)')
            ->execute([$in['provider'], ($in['name'] ?? '') ?: $in['provider'], Crypto::encrypt($in['access_key_id']),
                       Crypto::encrypt($in['access_key_secret']), json_encode($extra, JSON_UNESCAPED_UNICODE)]);
        Logger::op((int)$u['id'], $u['username'], 'account.create', $in['provider']);
        Http::ok();
    });
    $r->delete('/api/accounts/{id}', function ($id) {
        $u = Auth::requireRole('admin');
        $db = Database::i();
        $db->beginTransaction();
        try {
            $db->prepare('DELETE FROM sync_queue WHERE account_id=?')->execute([$id]);
            $db->prepare('DELETE FROM record_provider_map WHERE account_id=?')->execute([$id]);
            $db->prepare('DELETE FROM domain_providers WHERE account_id=?')->execute([$id]);
            $db->prepare('DELETE FROM cloud_accounts WHERE id=?')->execute([$id]);
            $db->commit();
        } catch (Throwable $e) {
            $db->rollBack();
            throw $e;
        }
        Logger::op((int)$u['id'], $u['username'], 'account.delete', (string)$id);
        Http::ok();
    });
    $r->post('/api/accounts/{id}/test', function ($id) {
        Auth::requireRole('admin', 'operator');
        $st = Database::i()->prepare('SELECT provider FROM cloud_accounts WHERE id=?');
        $st->execute([$id]);
        $acc = $st->fetch();
        if (!$acc) Http::fail('账号不存在', 404);
        try {
            Http::ok(ProviderFactory::makeById((int)$id)->ping());
        } catch (Throwable $e) {
            Http::fail($e->getMessage());
        }
    });

    // ---------- 域名 ----------
    $r->get('/api/domains', function () {
        Auth::requireUser();
        $db = Database::i();
        $rows = $db->query(
            'SELECT d.*, GROUP_CONCAT(CONCAT(dp.provider,\':\',IF(dp.sync_enabled=1,\'on\',\'off\'))) AS providers
             FROM domains d LEFT JOIN domain_providers dp ON dp.domain_id = d.id
             GROUP BY d.id ORDER BY d.id')->fetchAll();
        Http::ok($rows);
    });
    $r->post('/api/domains', function () {
        $u = Auth::requireRole('admin', 'operator');
        $in = Http::input();
        $domain = strtolower(trim($in['name'] ?? ''));
        if (!preg_match('/^([a-z0-9-]+\.)+[a-z]{2,}$/', $domain)) Http::fail('域名格式不正确');
        $db = Database::i();
        $st = $db->prepare('SELECT id FROM domains WHERE name=?');
        $st->execute([$domain]);
        if ($st->fetch()) Http::fail('域名已存在');

        $db->prepare('INSERT INTO domains (name,remark) VALUES (?,?)')->execute([$domain, $in['remark'] ?? null]);
        $domainId = (int)$db->lastInsertId();

        // 绑定到各厂商账号，逐一验证域名是否存在
        $accounts = $db->query('SELECT id,provider FROM cloud_accounts WHERE status=1')->fetchAll();
        $bind = $db->prepare('INSERT INTO domain_providers (domain_id,account_id,provider,verify_status,verify_message,last_verify_at) VALUES (?,?,?,?,?,NOW())');
        foreach ($accounts as $acc) {
            $ok = 'fail'; $msg = ''; $pid = null;
            try {
                $res = ProviderFactory::makeById((int)$acc['id'])->checkDomain($domain);
                $ok = $res['ok'] ? 'ok' : 'fail';
                $msg = $res['message'] ?? '';
                $pid = $res['domain_id'] ?? null;
            } catch (Throwable $e) {
                $msg = mb_substr($e->getMessage(), 0, 250);
            }
            $bind->execute([$domainId, $acc['id'], $acc['provider'], $ok, $msg]);
            if ($pid !== null) {
                $db->prepare('UPDATE domain_providers SET provider_domain_id=? WHERE domain_id=? AND account_id=?')
                    ->execute([(string)$pid, $domainId, $acc['id']]);
            }
        }
        // 如果任一厂商验证失败，允许手动强制添加
        Logger::op((int)$u['id'], $u['username'], 'domain.create', $domain);
        Http::ok(['domain_id' => $domainId]);
    });
    $r->post('/api/domains/{id}/verify', function ($id) {
        Auth::requireRole('admin', 'operator');
        Http::ok((new SyncService())->verifyDomain((int)$id));
    });
    $r->post('/api/domains/{id}/repair', function ($id) {
        $u = Auth::requireRole('admin', 'operator');
        $in = Http::input();
        $accountId = (int)($in['account_id'] ?? 0);
        $recordIds = $in['record_ids'] ?? [];
        if (!$accountId || !is_array($recordIds) || !$recordIds) Http::fail('参数错误');
        $st = Database::i()->prepare('SELECT 1 FROM domain_providers WHERE domain_id=? AND account_id=?');
        $st->execute([$id, $accountId]);
        if (!$st->fetch()) Http::fail('该平台未绑定到此域名');
        try {
            $results = (new SyncService())->pushToAccount((int)$id, $accountId, array_map('intval', $recordIds));
        } catch (Throwable $e) { Http::fail($e->getMessage()); }
        Logger::op((int)$u['id'], $u['username'], 'record.repair', (string)$id, ['account' => $accountId, 'count' => count($recordIds)]);
        Http::ok(['sync' => $results]);
    });
    $r->post('/api/domains/{id}/toggle', function ($id) {
        $u = Auth::requireRole('admin', 'operator');
        $in = Http::input();
        Database::i()->prepare('UPDATE domain_providers SET sync_enabled=? WHERE domain_id=? AND account_id=?')
            ->execute([$in['enabled'] ? 1 : 0, $id, $in['account_id']]);
        Logger::op((int)$u['id'], $u['username'], 'domain.toggle', $id, $in);
        Http::ok();
    });
    $r->delete('/api/domains/{id}', function ($id) {
        $u = Auth::requireRole('admin');
        $db = Database::i();
        $db->beginTransaction();
        try {
            $db->prepare('DELETE q FROM sync_queue q JOIN records r ON r.id=q.record_id WHERE r.domain_id=?')->execute([$id]);
            $db->prepare('DELETE rm FROM record_provider_map rm JOIN records r ON r.id=rm.record_id WHERE r.domain_id=?')->execute([$id]);
            $db->prepare('DELETE FROM records WHERE domain_id=?')->execute([$id]);
            $db->prepare('DELETE FROM domain_providers WHERE domain_id=?')->execute([$id]);
            $db->prepare('DELETE FROM domains WHERE id=?')->execute([$id]);
            $db->commit();
        } catch (Throwable $e) {
            $db->rollBack();
            throw $e;
        }
        Logger::op((int)$u['id'], $u['username'], 'domain.delete', (string)$id);
        Http::ok();
    });

    // ---------- 解析记录 ----------
    $r->get('/api/domains/{id}/records', function ($id) {
        Auth::requireUser();
        $db = Database::i();
        $page = max(1, (int)($_GET['page'] ?? 1));
        $size = min(200, max(20, (int)($_GET['size'] ?? 100)));
        $off = ($page - 1) * $size;

        $totalSt = $db->prepare('SELECT COUNT(*) c FROM records WHERE domain_id=?');
        $totalSt->execute([(int)$id]);
        $total = (int)$totalSt->fetch()['c'];

        $rows = $db->prepare(
            "SELECT r.*, GROUP_CONCAT(DISTINCT CONCAT(m.account_id, '|', COALESCE(m.provider, ca.provider), '|', m.sync_status)) AS sync_states
             FROM records r LEFT JOIN record_provider_map m ON m.record_id = r.id
             LEFT JOIN cloud_accounts ca ON ca.id = m.account_id
             WHERE r.domain_id=? GROUP BY r.id ORDER BY r.type, r.sub_domain, r.id LIMIT $size OFFSET $off");
        $rows->execute([$id]);
        header('Cache-Control: no-store, no-cache, must-revalidate, max-age=0');
        header('Pragma: no-cache');
        Http::ok(['items' => $rows->fetchAll(), 'total' => $total, 'page' => $page, 'size' => $size]);
    });
    $r->post('/api/domains/{id}/records', function ($id) use ($validateRecordConflict, $validateRecordInput) {
        $u = Auth::requireRole('admin', 'operator');
        $in = Http::input();
        $db = Database::i();
        [$sub, $type, $line, $value, $ttl, $priority] = $validateRecordInput($in);
        $validateRecordConflict($db, (int)$id, $sub, $type, $line, $value);
        $db->prepare('INSERT INTO records (domain_id,sub_domain,type,line,value,ttl,priority,remark) VALUES (?,?,?,?,?,?,?,?)')
            ->execute([$id, $sub, $type, $line, $value, $ttl, $priority, $in['remark'] ?? null]);
        $recordId = (int)$db->lastInsertId();
        $results = (new SyncService())->dispatch($recordId, 'create');
        Logger::op((int)$u['id'], $u['username'], 'record.create', (string)$recordId, $in);
        Http::ok(['record_id' => $recordId, 'sync' => $results]);
    });
    $r->post('/api/records/{id}', function ($id) use ($validateRecordConflict, $validateRecordInput) {
        $u = Auth::requireRole('admin', 'operator');
        $in = Http::input();
        $db = Database::i();
        $st = $db->prepare('SELECT domain_id FROM records WHERE id=?');
        $st->execute([(int)$id]);
        $recordDomainId = (int)$st->fetchColumn();
        if (!$recordDomainId) Http::fail('记录不存在', 404);
        [$sub, $type, $line, $value, $ttl, $priority] = $validateRecordInput($in);
        $validateRecordConflict($db, $recordDomainId, $sub, $type, $line, $value, (int)$id);
        $db->prepare('UPDATE records SET sub_domain=?,type=?,line=?,value=?,ttl=?,priority=?,remark=?,status=\'active\' WHERE id=?')
            ->execute([$sub, $type, $line, $value, $ttl, $priority, $in['remark'] ?? null, $id]);
        $results = (new SyncService())->dispatch((int)$id, 'update');
        Logger::op((int)$u['id'], $u['username'], 'record.update', (string)$id, $in);
        Http::ok(['sync' => $results]);
    });
    $r->delete('/api/records/{id}', function ($id) {
        $u = Auth::requireRole('admin', 'operator');
        $results = (new SyncService())->dispatch((int)$id, 'delete');
        $failed = array_filter($results, fn($result) => $result['status'] !== 'success');
        $db = Database::i();
        if (!$failed) {
            $db->prepare('DELETE FROM record_provider_map WHERE record_id=?')->execute([$id]);
            $db->prepare('DELETE FROM records WHERE id=?')->execute([$id]);
        } else {
            $db->prepare("UPDATE records SET status='disabled' WHERE id=?")->execute([$id]);
        }
        Logger::op((int)$u['id'], $u['username'], 'record.delete', (string)$id);
        Http::ok(['sync' => $results, 'pending' => (bool)$failed]);
    });
    $r->post('/api/records/batch-delete', function () {
        $u = Auth::requireRole('admin', 'operator');
        $ids = Http::input()['ids'] ?? [];
        if (!is_array($ids) || !$ids) Http::fail('ids 不能为空');
        $svc = new SyncService();
        $db = Database::i();
        $pending = [];
        foreach ($ids as $id) {
            $results = $svc->dispatch((int)$id, 'delete');
            if (array_filter($results, fn($result) => $result['status'] !== 'success')) {
                $db->prepare("UPDATE records SET status='disabled' WHERE id=?")->execute([(int)$id]);
                $pending[] = (int)$id;
                continue;
            }
            $db->prepare('DELETE FROM record_provider_map WHERE record_id=?')->execute([(int)$id]);
            $db->prepare('DELETE FROM records WHERE id=?')->execute([(int)$id]);
        }
        Logger::op((int)$u['id'], $u['username'], 'record.batch_delete', implode(',', $ids));
        Http::ok(['pending' => $pending]);
    });
    // 从云厂商拉取记录导入到本地（单向导入，不触发同步）
    $r->post('/api/domains/{id}/import-remote', function ($id) {
        $u = Auth::requireRole('admin', 'operator');
        $db = Database::i();
        $st = $db->prepare('SELECT * FROM domains WHERE id=?');
        $st->execute([$id]);
        $domain = $st->fetch();
        if (!$domain) Http::fail('域名不存在', 404);
        $dp = $db->prepare('SELECT dp.*, ca.provider FROM domain_providers dp JOIN cloud_accounts ca ON ca.id=dp.account_id WHERE dp.domain_id=? AND dp.sync_enabled=1');
        $dp->execute([$id]);
        $imported = 0;
        $errors = [];
        // 已有本地记录按归一化键索引，跨厂商/跨次拉取不重复插入
        $seen = [];
        $all = $db->prepare('SELECT id, sub_domain, type, line, value FROM records WHERE domain_id=?');
        $all->execute([$id]);
        foreach ($all->fetchAll() as $lr) {
            $seen[SyncService::recKey($lr['sub_domain'], $lr['type'], $lr['value'], (string)$lr['line'])] = (int)$lr['id'];
        }
        foreach ($dp->fetchAll() as $row) {
            try {
                $remote = ProviderFactory::makeById((int)$row['account_id'])->listRecords($domain['name']);
            } catch (Throwable $e) {
                $label = ['tencent' => '腾讯云', 'aliyun' => '阿里云', 'volc' => '火山引擎'][$row['provider']] ?? $row['provider'];
                $errors[] = $label . ': ' . $e->getMessage();
                continue;
            }
            foreach ($remote as $rr) {
                if (strtoupper((string)$rr['type']) === 'NS' && strtolower((string)$rr['sub']) === '@') continue;
                $line = SyncService::normLine((string)$rr['line']);
                $value = $rr['value'];
                $priority = $rr['priority'];
                if (strtoupper($rr['type']) === 'MX' && preg_match('/^(\d+)\s+(.+)$/', (string)$value, $m)) {
                    $priority = (int)$m[1];
                    $value = $m[2];
                }
                $key = SyncService::recKey($rr['sub'], $rr['type'], $value, $line);
                if (isset($seen[$key])) {
                    $recordId = $seen[$key];
                } else {
                    $db->prepare('INSERT INTO records (domain_id,sub_domain,type,line,value,ttl,priority) VALUES (?,?,?,?,?,?,?)')
                        ->execute([$id, $rr['sub'], $rr['type'], $line, $value, $rr['ttl'], $priority]);
                    $recordId = (int)$db->lastInsertId();
                    $seen[$key] = $recordId;
                    $imported++;
                }
                $db->prepare('INSERT INTO record_provider_map (record_id,account_id,provider,provider_record_id,sync_status,synced_at)
                              VALUES (?,?,?,?,?,NOW()) ON DUPLICATE KEY UPDATE provider=VALUES(provider), provider_record_id=VALUES(provider_record_id), sync_status=\'success\', synced_at=NOW()')
                    ->execute([$recordId, $row['account_id'], $row['provider'], $rr['id'], 'success']);
            }
        }
        $merged = (new SyncService())->mergeDuplicates((int)$id);
        Logger::op((int)$u['id'], $u['username'], 'record.import_remote', $domain['name'], ['count' => $imported, 'merged' => $merged]);
        Http::ok(['imported' => $imported, 'merged' => $merged, 'errors' => $errors]);
    });
    // 导入/导出
    $r->get('/api/domains/{id}/export', function ($id) {
        Auth::requireUser();
        $rows = Database::i()->prepare('SELECT sub_domain,type,line,value,ttl,priority FROM records WHERE domain_id=?');
        $rows->execute([$id]);
        $lines = ['sub_domain,type,line,value,ttl,priority'];
        foreach ($rows->fetchAll() as $r) {
            $lines[] = implode(',', array_map(fn($v) => '"' . str_replace('"', '""', (string)$v) . '"', [
                $r['sub_domain'], $r['type'], $r['line'], $r['value'], $r['ttl'], $r['priority'] ?? '',
            ]));
        }
        header('Content-Type: text/csv; charset=utf-8');
        header('Content-Disposition: attachment; filename="records_' . $id . '.csv"');
        echo "\xEF\xBB\xBF" . implode("\n", $lines);
        exit;
    });
    $r->post('/api/domains/{id}/import', function ($id) use ($validateRecordConflict, $checkRecordInput) {
        $u = Auth::requireRole('admin', 'operator');
        $in = Http::input();
        $csv = (string)($in['csv'] ?? '');
        if ($csv === '') Http::fail('csv 内容为空');
        if (strlen($csv) > 512 * 1024) Http::fail('单次导入不得超过 512KB，请分批');
        if (!preg_match('//u', $csv)) Http::fail('文件编码需为 UTF-8');

        $csv = str_replace("\xEF\xBB\xBF", '', $csv);
        $fh = fopen('php://temp', 'r+');
        fwrite($fh, $csv);
        rewind($fh);
        $header = fgetcsv($fh);
        if (!$header) { fclose($fh); Http::fail('缺少表头'); }
        $idx = array_flip(array_map(fn($h) => trim((string)$h), $header));
        if (!isset($idx['sub_domain'], $idx['type'], $idx['value'])) { fclose($fh); Http::fail('表头需包含 sub_domain,type,value'); }

        $db = Database::i();
        $dup = $db->prepare('SELECT id FROM records WHERE domain_id=? AND LOWER(sub_domain)=? AND type=? AND line=? AND value=?');
        $count = 0;
        $skipped = 0;
        $queue = [];
        while (($cols = fgetcsv($fh)) !== false) {
            if (count(array_filter($cols, fn($v) => $v !== null && trim((string)$v) !== '')) === 0) continue;
            $row = [
                'sub' => $cols[$idx['sub_domain']] ?? '@',
                'type' => $cols[$idx['type']] ?? '',
                'line' => $cols[$idx['line']] ?? '默认',
                'value' => $cols[$idx['value']] ?? '',
                'ttl' => (isset($idx['ttl']) ? $cols[$idx['ttl']] : 600),
                'priority' => isset($idx['priority']) ? $cols[$idx['priority']] : null,
            ];
            [$rowData, $rowErr] = $checkRecordInput($row);
            if ($rowErr !== '') { $skipped++; continue; }
            [$sub, $type, $line, $value, $ttl, $priority] = $rowData;
            if ($type === 'NS' && strtolower($sub) === '@') { $skipped++; continue; }
            $dup->execute([(int)$id, strtolower($sub), $type, $line, $value]);
            if ($dup->fetchColumn()) { $skipped++; continue; }
            $validateRecordConflict($db, (int)$id, $sub, $type, $line, $value);
            $db->prepare('INSERT INTO records (domain_id,sub_domain,type,line,value,ttl,priority) VALUES (?,?,?,?,?,?,?)')
                ->execute([$id, $sub, $type, $line, $value, $ttl, $priority]);
            $queue[] = (int)$db->lastInsertId();
            $count++;
        }
        fclose($fh);
        $results = (new SyncService())->dispatchMany($queue, 'create');
        $failed = count(array_filter($results, fn($r2) => $r2['status'] !== 'success'));
        Logger::op((int)$u['id'], $u['username'], 'record.import', (string)$id, ['count' => $count, 'skipped' => $skipped, 'failed' => $failed]);
        Http::ok(['imported' => $count, 'skipped' => $skipped, 'failed' => $failed, 'sync' => $results]);
    });

    // ---------- 同步 ----------
    $r->get('/api/sync/logs', function () {
        Auth::requireUser();
        $st = Database::i()->prepare(
            'SELECT l.*, ca.provider AS p, ca.name AS account_name FROM sync_logs l
             JOIN cloud_accounts ca ON ca.id = l.account_id ORDER BY l.id DESC LIMIT 200');
        $st->execute();
        Http::ok($st->fetchAll());
    });
    $r->delete('/api/sync/logs', function () {
        $u = Auth::requireRole('admin');
        $st = Database::i()->prepare('DELETE FROM sync_logs');
        $st->execute();
        Logger::op((int)$u['id'], $u['username'], 'synclog.clear', '', ['rows' => $st->rowCount()]);
        Http::ok();
    });
    $r->get('/api/sync/queue', function () {
        Auth::requireUser();
        $st = Database::i()->prepare(
            'SELECT q.*, ca.provider FROM sync_queue q JOIN cloud_accounts ca ON ca.id=q.account_id
             WHERE q.status IN (\'pending\',\'failed\') ORDER BY q.id DESC LIMIT 100');
        $st->execute();
        Http::ok($st->fetchAll());
    });
    $r->post('/api/sync/retry-now', function () {
        Auth::requireRole('admin', 'operator');
        $db = Database::i();
        $db->exec("UPDATE sync_queue SET status='pending', retry_count=0, next_retry_at=NULL WHERE status IN ('pending','failed')");
        $done = (new SyncService())->processPendingQueue(100);
        Http::ok(['count' => count($done), 'items' => $done]);
    });
    $r->delete('/api/sync/queue', function () {
        $u = Auth::requireRole('admin', 'operator');
        $st = Database::i()->prepare("DELETE FROM sync_queue WHERE status IN ('pending','failed')");
        $st->execute();
        $count = $st->rowCount();
        Logger::op((int)$u['id'], $u['username'], 'syncqueue.clear', '', ['rows' => $count]);
        Http::ok(['count' => $count]);
    });

    // ---------- 操作日志 ----------
    $r->get('/api/operation-logs', function () {
        Auth::requireRole('admin');
        $st = Database::i()->query('SELECT * FROM operation_logs ORDER BY id DESC LIMIT 200');
        Http::ok($st->fetchAll());
    });
    $r->delete('/api/operation-logs', function () {
        $u = Auth::requireRole('admin');
        $st = Database::i()->prepare('DELETE FROM operation_logs');
        $st->execute();
        Logger::op((int)$u['id'], $u['username'], 'oplog.clear', '', ['rows' => $st->rowCount()]);
        Http::ok();
    });

    // ---------- 备份与恢复 ----------
    $r->get('/api/backups', function () {
        Auth::requireRole('admin');
        Http::ok(Database::i()->query('SELECT id,name,record_count,created_by,created_at FROM backups ORDER BY id DESC LIMIT 100')->fetchAll());
    });
    $r->post('/api/backups', function () {
        $u = Auth::requireRole('admin');
        $db = Database::i();
        $data = json_encode([
            'schema_version' => 2,
            'backed_at' => date('Y-m-d H:i:s'),
            'domains' => $db->query('SELECT * FROM domains')->fetchAll(),
            'domain_providers' => $db->query('SELECT * FROM domain_providers')->fetchAll(),
            'records' => $db->query('SELECT * FROM records')->fetchAll(),
            'record_provider_map' => $db->query('SELECT * FROM record_provider_map')->fetchAll(),
        ], JSON_UNESCAPED_UNICODE);
        $gz = gzencode((string)$data, 6);
        if ($gz === false) Http::fail('备份压缩失败');
        $db->prepare('INSERT INTO backups (name,data,record_count,created_by,is_gzip) VALUES (?,?,?,?,1)')
            ->execute(['手动备份 ' . date('Y-m-d H:i'), $gz,
                       (int)$db->query('SELECT COUNT(*) c FROM records')->fetch()['c'], $u['username']]);
        Logger::op((int)$u['id'], $u['username'], 'backup.create');
        Http::ok();
    });
    $r->post('/api/backups/delete', function () {
        $u = Auth::requireRole('admin');
        $ids = Http::input()['ids'] ?? [];
        if (!is_array($ids) || !$ids) Http::fail('未选择备份');
        $ids = array_values(array_filter(array_map('intval', $ids), fn($v) => $v > 0));
        if (!$ids) Http::fail('未选择备份');
        $ph = implode(',', array_fill(0, count($ids), '?'));
        $st = Database::i()->prepare("DELETE FROM backups WHERE id IN ($ph)");
        $st->execute($ids);
        Logger::op((int)$u['id'], $u['username'], 'backup.delete', implode(',', $ids));
        Http::ok(['deleted' => $st->rowCount()]);
    });
    $r->post('/api/backups/{id}/restore', function ($id) {
        $u = Auth::requireRole('admin');
        $db = Database::i();
        $st = $db->prepare('SELECT * FROM backups WHERE id=?');
        $st->execute([$id]);
        $bk = $st->fetch();
        if (!$bk) Http::fail('备份不存在', 404);

        $raw = !empty($bk['is_gzip']) ? gzdecode((string)$bk['data']) : (string)$bk['data'];
        $data = $raw !== false ? json_decode($raw, true) : null;
        if (!is_array($data)) Http::fail('备份数据损坏，无法恢复');
        if (isset($data['schema_version']) && (int)$data['schema_version'] !== 2) {
            Http::fail('备份结构版本不匹配（' . (int)$data['schema_version'] . '/2），请使用同版本备份恢复');
        }

        $before = json_encode([
            'schema_version' => 2,
            'backed_at' => date('Y-m-d H:i:s'),
            'domains' => $db->query('SELECT * FROM domains')->fetchAll(),
            'domain_providers' => $db->query('SELECT * FROM domain_providers')->fetchAll(),
            'records' => $db->query('SELECT * FROM records')->fetchAll(),
            'record_provider_map' => $db->query('SELECT * FROM record_provider_map')->fetchAll(),
        ], JSON_UNESCAPED_UNICODE);
        $db->prepare('INSERT INTO backups (name,data,record_count,created_by,is_gzip) VALUES (?,?,?,?,1)')
            ->execute(['恢复前快照 ' . date('Y-m-d H:i:s') . ' (备份#' . (int)$id . ')', gzencode((string)$before, 6),
                       (int)$db->query('SELECT COUNT(*) c FROM records')->fetch()['c'], $u['username']]);

        $now = date('Y-m-d H:i:s');
        $db->beginTransaction();
        try {
            $db->exec('DELETE FROM sync_queue');
            foreach (['record_provider_map', 'records', 'domain_providers', 'domains'] as $t) {
                $db->exec("DELETE FROM $t");
            }
            foreach ($data['domains'] ?? [] as $row) {
                $db->prepare('INSERT INTO domains (id,name,status,remark,created_at) VALUES (?,?,?,?,?)')
                    ->execute([$row['id'], $row['name'], $row['status'] ?? 'active', $row['remark'] ?? null, $row['created_at'] ?? $now]);
            }
            foreach ($data['domain_providers'] ?? [] as $row) {
                $db->prepare('INSERT INTO domain_providers (id,domain_id,account_id,provider,provider_domain_id,sync_enabled,last_verify_at,verify_status,verify_message) VALUES (?,?,?,?,?,?,?,?,?)')
                    ->execute([$row['id'], $row['domain_id'], $row['account_id'], $row['provider'], $row['provider_domain_id'] ?? null,
                               (int)($row['sync_enabled'] ?? 1), $row['last_verify_at'] ?? null, $row['verify_status'] ?? 'unknown',
                               $row['verify_message'] ?? null]);
            }
            foreach ($data['records'] ?? [] as $row) {
                $db->prepare('INSERT INTO records (id,domain_id,sub_domain,type,line,value,ttl,priority,remark,status,updated_at,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)')
                    ->execute([$row['id'], $row['domain_id'], $row['sub_domain'] ?? '@', $row['type'], $row['line'] ?? '默认',
                               $row['value'], (int)($row['ttl'] ?? 600), $row['priority'] ?? null, $row['remark'] ?? null,
                               $row['status'] ?? 'active', $row['updated_at'] ?? $now, $row['created_at'] ?? $now]);
            }
            foreach ($data['record_provider_map'] ?? [] as $row) {
                $db->prepare('INSERT INTO record_provider_map (id,record_id,account_id,provider,provider_record_id,sync_status,sync_message,synced_at,retry_count) VALUES (?,?,?,?,?,?,?,?,?)')
                    ->execute([$row['id'], $row['record_id'], $row['account_id'], $row['provider'], $row['provider_record_id'] ?? null,
                               $row['sync_status'] ?? 'pending', $row['sync_message'] ?? null, $row['synced_at'] ?? null,
                               (int)($row['retry_count'] ?? 0)]);
            }
            $db->exec("UPDATE record_provider_map SET sync_status='pending', sync_message='恢复备份，待远端校验' WHERE sync_status='success'");
            $db->commit();
        } catch (Throwable $e) {
            $db->rollBack();
            Http::fail('恢复失败: ' . $e->getMessage());
        }
        Logger::op((int)$u['id'], $u['username'], 'backup.restore', (string)$id);
        Http::ok(['need_verify' => true]);
    });

    // ---------- 监控 ----------
    $r->get('/api/monitor/summary', function () {
        Auth::requireUser();
        $db = Database::i();
        $q = fn($sql) => (int)$db->query($sql)->fetch()['c'];
        Http::ok([
            'domains' => $q('SELECT COUNT(*) c FROM domains'),
            'records' => $q('SELECT COUNT(*) c FROM records'),
            'pending_queue' => $q('SELECT COUNT(*) c FROM sync_queue WHERE status=\'pending\''),
            'failed_queue' => $q('SELECT COUNT(*) c FROM sync_queue WHERE status=\'failed\''),
            'failed_sync_24h' => $q('SELECT COUNT(*) c FROM sync_logs WHERE status=\'failed\' AND created_at > DATE_SUB(NOW(), INTERVAL 24 HOUR)'),
            'success_sync_24h' => $q('SELECT COUNT(*) c FROM sync_logs WHERE status=\'success\' AND created_at > DATE_SUB(NOW(), INTERVAL 24 HOUR)'),
        ]);
    });
};
