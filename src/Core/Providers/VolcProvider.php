<?php
namespace App\Core\Providers;

// 火山引擎 DNS，AWS V4 风格签名
class VolcProvider extends BaseProvider
{
    private const HOST = 'dns.volcengineapi.com';
    private const SERVICE = 'dns';
    private const VERSION = '2018-08-01';

    public function providerName(): string { return 'volc'; }

    private const WRITE_ACTIONS = ['CreateRecord', 'UpdateRecord', 'DeleteRecord'];

    public function buildRequest(string $action, array $query): array
    {
        $ts = time();
        $date = gmdate('Ymd', $ts);
        $dt = gmdate('Ymd\THis\Z', $ts);
        $region = 'cn-beijing';

        $isWrite = in_array($action, self::WRITE_ACTIONS, true);
        if ($isWrite) {
            $body = json_encode($query, JSON_UNESCAPED_SLASHES);
            $qp = ['Action' => $action, 'Version' => self::VERSION];
        } else {
            $body = null;
            $qp = $query + ['Action' => $action, 'Version' => self::VERSION];
        }
        ksort($qp);
        $canonicalQuery = implode('&', array_map(
            fn($k, $v) => rawurlencode($k) . '=' . rawurlencode((string)$v),
            array_keys($qp), array_values($qp)
        ));

        $payloadHash = hash('sha256', $body ?? '');

        $canonicalHeaders = "content-type:application/json\nhost:" . self::HOST . "\nx-content-sha256:$payloadHash\nx-date:$dt\n";
        $signedHeaders = 'content-type;host;x-content-sha256;x-date';
        $method = $isWrite ? 'POST' : 'GET';
        $canonicalRequest = "$method\n/\n$canonicalQuery\n$canonicalHeaders\n$signedHeaders\n$payloadHash";

        $credScope = "$date/$region/" . self::SERVICE . "/request";
        $strToSign = "HMAC-SHA256\n$dt\n$credScope\n" . hash('sha256', $canonicalRequest);

        $kDate = hash_hmac('sha256', $date, $this->sk, true);
        $kRegion = hash_hmac('sha256', $region, $kDate, true);
        $kService = hash_hmac('sha256', self::SERVICE, $kRegion, true);
        $kSigning = hash_hmac('sha256', 'request', $kService, true);
        $signature = hash_hmac('sha256', $strToSign, $kSigning);

        $sessionToken = $this->extra['session_token'] ?? '';
        $authHeader = "HMAC-SHA256 Credential={$this->ak}/$credScope, SignedHeaders=$signedHeaders, Signature=$signature";
        $url = 'https://' . self::HOST . '/?' . $canonicalQuery;

        $headers = [
            'Content-Type: application/json',
            'X-Date: ' . $dt,
            'X-Content-Sha256: ' . $payloadHash,
            'Authorization: ' . $authHeader,
        ];
        if ($sessionToken) $headers[] = 'X-Security-Session-Token: ' . $sessionToken;

        return ['method' => $method, 'url' => $url, 'headers' => $headers, 'body' => $body];
    }

    public function parseResponse(string $action, int $code, string $resp): array
    {
        $j = json_decode($resp, true) ?? [];
        if (isset($j['ResponseMetadata']['Error'])) {
            $e = $j['ResponseMetadata']['Error'];
            $errorCode = (string)($e['Code'] ?? '');
            if ($action === 'DeleteRecord' && $errorCode === 'ErrDBNotFound') {
                return [];
            }
            throw new ProviderException('火山引擎: ' . $errorCode . ' ' . ($e['Message'] ?? ''));
        }
        return $this->expect($j['Result'] ?? [], $code, $resp);
    }

    private function call(string $action, array $query): array
    {
        $req = $this->buildRequest($action, $query);
        [$code, $resp] = $this->http($req['method'], $req['url'], $req['headers'], $req['body']);
        return $this->parseResponse($action, $code, $resp);
    }

    public function ping(): array
    {
        $r = $this->call('ListZones', ['PageSize' => 1]);
        return ['ok' => true, 'domains' => $r['TotalCount'] ?? count($r['Zones'] ?? [])];
    }

    public function checkDomain(string $domain): array
    {
        $r = $this->call('CheckDomain', ['Key' => 'domain', 'Value' => $domain]);
        $isLB = $r['IsLB'] ?? true;
        if (!$isLB) return ['ok' => false, 'message' => '域名不存在'];
        return ['ok' => true, 'domain_id' => $domain];
    }

    public function listRecords(string $domain): array
    {
        $out = [];
        $page = 1;
        do {
            $r = $this->call('ListRecords', ['ZID' => (int)($this->zid($domain)), 'PageNumber' => $page, 'PageSize' => 100]);
            foreach ($r['Records'] ?? [] as $rec) {
                $out[] = [
                    'id' => (string)$rec['RecordID'],
                    'sub' => $rec['Host'],
                    'type' => $rec['Type'],
                    'line' => $rec['Line'],
                    'value' => $rec['Value'],
                    'ttl' => (int)$rec['TTL'],
                    'priority' => $rec['Weight'] ?? null,
                ];
            }
            $total = $r['TotalCount'] ?? count($out);
            $page++;
        } while (count($out) < $total && !empty($r['Records']));
        return $out;
    }

    private function zid(string $domain): int
    {
        $target = rtrim(strtolower($domain), '.');
        $page = 1;
        do {
            $r = $this->call('ListZones', ['PageNumber' => $page, 'PageSize' => 100]);
            $zones = $r['Zones'] ?? [];
            foreach ($zones as $z) {
                if (rtrim(strtolower((string)($z['ZoneName'] ?? '')), '.') === $target) return (int)$z['ZID'];
            }
            $total = (int)($r['TotalCount'] ?? count($zones));
            $page++;
        } while ($zones && ($page - 1) * 100 < $total);
        throw new ProviderException('火山引擎: 该账号下不存在域名 ' . $domain);
    }

    public function opParams(string $op, string $domain, array $record, ?string $remoteId): array
    {
        return match ($op) {
            'create' => ['CreateRecord', $this->writeParams($domain, $record)],
            'update' => $remoteId !== null
                ? ['UpdateRecord', $this->writeParams($domain, $record, (string)$remoteId)]
                : ['CreateRecord', $this->writeParams($domain, $record)],
            'delete' => $remoteId !== null && $remoteId !== ''
                ? ['DeleteRecord', ['RecordID' => (string)$remoteId]]
                : throw new ProviderException('火山引擎: 缺少远端记录 ID'),
        };
    }

    private function writeParams(string $domain, array $r, ?string $recordId = null): array
    {
        $p = [];
        if ($recordId !== null) $p['RecordID'] = (string)$recordId;
        else $p['ZID'] = (int)$this->zid($domain);
        $p['Host'] = $r['sub'];
        $p['Type'] = $r['type'];
        $val = $this->writeValue($r['type'], (string)$r['value']);
        // 火山 MX 的优先级存放在记录值内（格式 "10 mx.example.com"）
        if (strtoupper($r['type']) === 'MX' && !empty($r['priority']) && !preg_match('/^\d+\s+/', $val)) {
            $val = (int)$r['priority'] . ' ' . $val;
        }
        $p['Value'] = $val;
        $p['TTL'] = $this->writeTtl($r['ttl']);
        $p['Line'] = $this->lineValue($r['line'] ?? null);
        return $p;
    }

    private function lineValue(?string $line): string
    {
        if ($line === null || $line === '' || $line === '默认' || strcasecmp($line, 'default') === 0) return 'default';
        return $line;
    }

    public function createRecord(string $domain, array $r): array
    {
        $res = $this->call('CreateRecord', $this->writeParams($domain, $r));
        return ['id' => (string)($res['RecordID'] ?? '')];
    }

    public function updateRecord(string $domain, string $recordId, array $r): array
    {
        $this->call('UpdateRecord', $this->writeParams($domain, $r, $recordId));
        return ['id' => $recordId];
    }

    public function deleteRecord(string $domain, string $recordId): array
    {
        $this->call('DeleteRecord', ['RecordID' => (string)$recordId]);
        return ['id' => $recordId];
    }
}
