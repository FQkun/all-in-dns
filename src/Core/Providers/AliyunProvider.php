<?php
namespace App\Core\Providers;

// 阿里云 Alidns，RPC 风格 HMAC-SHA1 签名
class AliyunProvider extends BaseProvider
{
    private const HOST = 'alidns.aliyuncs.com';
    private const VERSION = '2015-01-09';

    public function providerName(): string { return 'aliyun'; }

    public function buildRequest(string $action, array $params): array
    {
        $params = array_merge($params, [
            'Action' => $action,
            'Version' => self::VERSION,
            'Format' => 'JSON',
            'AccessKeyId' => $this->ak,
            'SignatureMethod' => 'HMAC-SHA1',
            'SignatureVersion' => '1.0',
            'SignatureNonce' => bin2hex(random_bytes(16)),
            'Timestamp' => gmdate('Y-m-d\TH:i:s\Z'),
        ]);
        ksort($params);
        $canonical = implode('&', array_map(
            fn($k, $v) => rawurlencode($k) . '=' . rawurlencode((string)$v),
            array_keys($params), array_values($params)
        ));
        $strToSign = 'GET&' . rawurlencode('/') . '&' . rawurlencode($canonical);
        $sig = base64_encode(hash_hmac('sha1', $strToSign, $this->sk . '&', true));
        $url = 'https://' . self::HOST . '/?' . $canonical . '&Signature=' . rawurlencode($sig);

        return ['method' => 'GET', 'url' => $url, 'headers' => [], 'body' => null];
    }

    public function parseResponse(string $action, int $code, string $resp): array
    {
        $j = json_decode($resp, true) ?? [];
        if (isset($j['Code']) || isset($j['Message'])) {
            $c = (string)($j['Code'] ?? 'Error');
            if ($action === 'DeleteDomainRecord' && in_array($c, ['DomainRecordNotBelongToUser', 'DomainRecord.NotExist'], true)) {
                return [];
            }
            if ($c === 'InvalidDomainName.NoExist' || $c === 'DomainRecord.NotExist') {
                throw new ProviderException('阿里云: 该账号下不存在此域名（云解析 DNS 未添加）');
            }
            throw new ProviderException('阿里云: ' . $c . ' ' . ($j['Message'] ?? ''));
        }
        return $this->expect($j, $code, $resp);
    }

    private function call(string $action, array $params): array
    {
        $req = $this->buildRequest($action, $params);
        [$code, $resp] = $this->http($req['method'], $req['url'], $req['headers'], $req['body']);
        return $this->parseResponse($action, $code, $resp);
    }

    public function ping(): array
    {
        $r = $this->call('DescribeDomains', ['PageSize' => 1]);
        return ['ok' => true, 'domains' => $r['TotalCount'] ?? 0];
    }

    public function checkDomain(string $domain): array
    {
        $r = $this->call('DescribeDomainInfo', ['DomainName' => $domain]);
        if (empty($r['DomainId'])) return ['ok' => false, 'message' => '域名不存在'];
        return ['ok' => true, 'domain_id' => $r['DomainId']];
    }

    public function listRecords(string $domain): array
    {
        $out = [];
        $pageNum = 1;
        do {
            $r = $this->call('DescribeDomainRecords', ['DomainName' => $domain, 'PageSize' => 100, 'PageNumber' => $pageNum]);
            foreach ($r['DomainRecords']['Record'] ?? [] as $rec) {
                $out[] = [
                    'id' => (string)$rec['RecordId'],
                    'sub' => $rec['RR'],
                    'type' => $rec['Type'],
                    'line' => LineMapper::from('aliyun', (string)$rec['Line']),
                    'value' => $rec['Value'],
                    'ttl' => (int)$rec['TTL'],
                    'priority' => $rec['Priority'] ?? null,
                ];
            }
            $total = $r['TotalCount'] ?? count($out);
            $pageNum++;
        } while (count($out) < $total && !empty($r['DomainRecords']['Record']));
        return $out;
    }

    public function opParams(string $op, string $domain, array $record, ?string $remoteId): array
    {
        return match ($op) {
            'create' => ['AddDomainRecord', $this->writeParams($record, null, $domain)],
            'update' => $remoteId !== null
                ? ['UpdateDomainRecord', $this->writeParams($record, $remoteId)]
                : ['AddDomainRecord', $this->writeParams($record, null, $domain)],
            'delete' => ['DeleteDomainRecord', ['RecordId' => (string)$remoteId]],
        };
    }

    private function writeParams(array $r, ?string $recordId = null, ?string $domain = null): array
    {
        $p = [];
        if ($domain !== null) $p['DomainName'] = $domain;
        if ($recordId !== null) $p['RecordId'] = $recordId;
        $p['RR'] = $r['sub'];
        $p['Type'] = $r['type'];
        $p['Value'] = $this->writeValue($r['type'], (string)$r['value']);
        $p['TTL'] = $this->writeTtl($r['ttl']);
        $p['Line'] = $this->lineValue($r['line'] ?? null);
        if ($r['priority']) $p['Priority'] = $r['priority'];
        return $p;
    }

    private function lineValue(?string $line): string
    {
        return LineMapper::to('aliyun', $line);
    }

    public function createRecord(string $domain, array $r): array
    {
        $res = $this->call('AddDomainRecord', $this->writeParams($r, null, $domain));
        return ['id' => (string)($res['RecordId'] ?? '')];
    }

    public function updateRecord(string $domain, string $recordId, array $r): array
    {
        $this->call('UpdateDomainRecord', $this->writeParams($r, $recordId));
        return ['id' => $recordId];
    }

    public function deleteRecord(string $domain, string $recordId): array
    {
        $this->call('DeleteDomainRecord', ['RecordId' => $recordId]);
        return ['id' => $recordId];
    }
}
