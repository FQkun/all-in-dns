<?php
namespace App\Core\Providers;

// 腾讯云 DNSPod，TC3-HMAC-SHA256 签名
class TencentProvider extends BaseProvider
{
    private const HOST = 'dnspod.tencentcloudapi.com';
    private const SERVICE = 'dnspod';
    private const VERSION = '2021-03-23';

    public function providerName(): string { return 'tencent'; }

    public function buildRequest(string $action, array $params): array
    {
        $ts = time();
        $date = gmdate('Y-m-d', $ts);
        $payload = json_encode($params, JSON_UNESCAPED_SLASHES);

        $hashedBody = hash('SHA256', $payload);
        $canonical = "POST\n/\n\n"
            . "content-type:application/json; charset=utf-8\n"
            . "host:" . self::HOST . "\n"
            . "x-tc-action:" . strtolower($action) . "\n\n"
            . "content-type;host;x-tc-action\n"
            . $hashedBody;
        $credScope = "$date/" . self::SERVICE . "/tc3_request";
        $strToSign = "TC3-HMAC-SHA256\n$ts\n$credScope\n" . hash('SHA256', $canonical);

        $kDate = hash_hmac('SHA256', $date, 'TC3' . $this->sk, true);
        $kService = hash_hmac('SHA256', self::SERVICE, $kDate, true);
        $kSigning = hash_hmac('SHA256', 'tc3_request', $kService, true);
        $signature = hash_hmac('SHA256', $strToSign, $kSigning);

        $auth = "TC3-HMAC-SHA256 Credential={$this->ak}/$credScope, SignedHeaders=content-type;host;x-tc-action, Signature=$signature";

        return [
            'method' => 'POST',
            'url' => 'https://' . self::HOST,
            'headers' => [
                'Content-Type: application/json; charset=utf-8',
                'X-TC-Action: ' . $action,
                'X-TC-Version: ' . self::VERSION,
                'X-TC-Timestamp: ' . $ts,
                'Authorization: ' . $auth,
            ],
            'body' => $payload,
        ];
    }

    public function parseResponse(string $action, int $code, string $resp): array
    {
        $j = json_decode($resp, true) ?? [];
        if (isset($j['Response']['Error'])) {
            $err = $j['Response']['Error'];
            if ($action === 'DeleteRecord' && in_array((string)$err['Code'], ['InvalidParameter.RecordIdInvalid', 'ResourceNotFound.Record'], true)) {
                return [];
            }
            if (str_starts_with((string)$err['Code'], 'ResourceNotFound')) {
                throw new ProviderException('腾讯云: 该账号下不存在此域名（DNSPod 未添加）');
            }
            throw new ProviderException('腾讯云: ' . $err['Code'] . ' ' . $err['Message']);
        }
        return $this->expect($j['Response'] ?? [], $code, $resp);
    }

    private function call(string $action, array $params): array
    {
        $req = $this->buildRequest($action, $params);
        [$code, $resp] = $this->http($req['method'], $req['url'], $req['headers'], $req['body']);
        return $this->parseResponse($action, $code, $resp);
    }

    public function ping(): array
    {
        $r = $this->call('DescribeDomainList', ['Limit' => 30]);
        $count = $r['TotalCount'] ?? 0;
        if (!$count) $count = count($r['DomainList'] ?? []);
        return ['ok' => true, 'domains' => $count];
    }

    public function checkDomain(string $domain): array
    {
        $r = $this->call('DescribeDomain', ['Name' => $domain]);
        return ['ok' => true, 'domain_id' => $r['DomainInfo']['Id'] ?? null];
    }

    public function listRecords(string $domain): array
    {
        $out = [];
        $offset = 0;
        do {
            $r = $this->call('DescribeRecordList', ['Domain' => $domain, 'Offset' => $offset, 'Limit' => 100]);
            foreach ($r['RecordList'] ?? [] as $rec) {
                if ($rec['Type'] === 'NS' && $rec['Name'] === '@') continue;
                $out[] = [
                    'id' => (string)$rec['RecordId'],
                    'sub' => $rec['Name'],
                    'type' => $rec['Type'],
                    'line' => $rec['Line'],
                    'value' => $rec['Value'],
                    'ttl' => (int)$rec['TTL'],
                    'priority' => $rec['MX'] ?? null,
                ];
            }
            $total = $r['RecordCountInfo']['TotalCount'] ?? count($out);
            $offset += 100;
        } while ($offset < $total && !empty($r['RecordList']));
        return $out;
    }

    public function opParams(string $op, string $domain, array $record, ?string $remoteId): array
    {
        return match ($op) {
            'create' => ['CreateRecord', $this->writeParams($domain, $record)],
            'update' => $remoteId !== null
                ? ['ModifyRecord', $this->writeParams($domain, $record, $remoteId)]
                : ['CreateRecord', $this->writeParams($domain, $record)],
            'delete' => ['DeleteRecord', ['Domain' => $domain, 'RecordId' => (int)$remoteId]],
        };
    }

    private function writeParams(string $domain, array $r, ?string $recordId = null): array
    {
        $p = ['Domain' => $domain];
        if ($recordId !== null) $p['RecordId'] = (int)$recordId;
        $p['SubDomain'] = $r['sub'];
        $p['RecordType'] = $r['type'];
        $p['RecordLine'] = $this->lineValue($r['line'] ?? null);
        $p['Value'] = $this->writeValue($r['type'], (string)$r['value']);
        $p['TTL'] = $this->writeTtl($r['ttl']);
        if ($r['priority']) $p['MX'] = $r['priority'];
        return $p;
    }

    private function lineValue(?string $line): string
    {
        if ($line === null || $line === '' || $line === '默认' || strcasecmp($line, 'default') === 0) return '默认';
        return $line;
    }

    public function createRecord(string $domain, array $r): array
    {
        $res = $this->call('CreateRecord', $this->writeParams($domain, $r));
        return ['id' => (string)($res['RecordId'] ?? '')];
    }

    public function updateRecord(string $domain, string $recordId, array $r): array
    {
        $this->call('ModifyRecord', $this->writeParams($domain, $r, $recordId));
        return ['id' => $recordId];
    }

    public function deleteRecord(string $domain, string $recordId): array
    {
        $this->call('DeleteRecord', ['Domain' => $domain, 'RecordId' => (int)$recordId]);
        return ['id' => $recordId];
    }
}
