<?php
namespace App\Core\Providers;

abstract class BaseProvider
{
    public function __construct(protected string $ak, protected string $sk, protected array $extra = []) {}

    abstract public function providerName(): string;
    /** 连通性测试（无需具体域名） */
    abstract public function ping(): array;
    /** 验证域名是否存在于该平台 */
    abstract public function checkDomain(string $domain): array;
    /** 拉取该域名下全部解析记录，统一格式: [provider_record_id, sub, type, line, value, ttl, priority] */
    abstract public function listRecords(string $domain): array;
    abstract public function createRecord(string $domain, array $r): array;   // 返回 ['id'=>...]
    abstract public function updateRecord(string $domain, string $recordId, array $r): array;
    abstract public function deleteRecord(string $domain, string $recordId): array;
    abstract public function buildRequest(string $action, array $params): array;
    abstract public function parseResponse(string $action, int $code, string $resp): array;
    abstract public function opParams(string $op, string $domain, array $record, ?string $remoteId): array;

    protected function http(string $method, string $url, array $headers = [], ?string $body = null): array
    {
        $h = [];
        $hasCt = false;
        foreach ($headers as $k => $v) {
            if (is_int($k)) {
                if (stripos($v, 'content-type:') === 0) $hasCt = true;
                $h[] = $v;
            } else {
                if (strcasecmp($k, 'content-type') === 0) $hasCt = true;
                $h[] = "$k: $v";
            }
        }
        if ($body !== null && !$hasCt) $h[] = 'Content-Type: application/json';
        $ch = curl_init($url);
        curl_setopt_array($ch, [
            CURLOPT_CUSTOMREQUEST => $method,
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_HTTPHEADER => $h,
            CURLOPT_TIMEOUT => 15,
            CURLOPT_CONNECTTIMEOUT => 5,
        ]);
        if ($body !== null) curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
        $resp = curl_exec($ch);
        $code = (int)curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
        $cost = (int)round(curl_getinfo($ch, CURLINFO_TOTAL_TIME) * 1000);
        $err = curl_error($ch);
        curl_close($ch);
        if ($resp === false) throw new ProviderException("HTTP请求失败: $err");
        return [$code, $resp, $cost];
    }

    protected function expect(array $result, int $code, string $resp): array
    {
        if ($code < 200 || $code >= 300) throw new ProviderException("HTTP $code: " . mb_substr($resp, 0, 300));
        return $result;
    }

    /** 各厂商 TTL 普遍限制 600-86400，统一收敛 */
    protected function writeTtl(mixed $ttl): int
    {
        $t = (int)($ttl ?: 600);
        return max(600, min(86400, $t));
    }

    /** 主机名类记录值去尾点（部分厂商解析带尾点的值会报错） */
    protected function writeValue(string $type, string $value): string
    {
        $v = trim($value);
        if (in_array(strtoupper($type), ['CNAME', 'MX', 'NS'], true)) $v = rtrim($v, '.');
        return $v;
    }

    public static function callParallel(array $tasks): array
    {
        $results = [];
        $mh = curl_multi_init();
        $handles = [];
        $taskParams = [];
        foreach (array_values($tasks) as $i => $task) {
            $taskParams[$i] = $task['params'];
            $provider = $task['provider'];
            $action = $task['action'];
            try {
                $req = $provider->buildRequest($action, $task['params']);
            } catch (\Throwable $e) {
                $results[$i] = ['error' => $e->getMessage()];
                continue;
            }
            $h = [];
            $hasCt = false;
            foreach ($req['headers'] as $k => $v) {
                if (is_int($k)) {
                    if (stripos($v, 'content-type:') === 0) $hasCt = true;
                    $h[] = $v;
                } else {
                    if (strcasecmp($k, 'content-type') === 0) $hasCt = true;
                    $h[] = "$k: $v";
                }
            }
            $body = $req['body'] ?? null;
            if ($body !== null && !$hasCt) $h[] = 'Content-Type: application/json';
            $ch = curl_init($req['url']);
            curl_setopt_array($ch, [
                CURLOPT_CUSTOMREQUEST => $req['method'],
                CURLOPT_RETURNTRANSFER => true,
                CURLOPT_HTTPHEADER => $h,
                CURLOPT_TIMEOUT => 15,
                CURLOPT_CONNECTTIMEOUT => 5,
            ]);
            if ($body !== null) curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
            curl_multi_add_handle($mh, $ch);
            $handles[$i] = ['ch' => $ch, 'provider' => $provider, 'action' => $action];
        }

        $running = null;
        do {
            $status = curl_multi_exec($mh, $running);
            if ($running) curl_multi_select($mh, 1.0);
        } while ($running && $status === CURLM_OK);

        foreach ($handles as $i => $item) {
            $ch = $item['ch'];
            $resp = curl_multi_getcontent($ch);
            $code = (int)curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
            $cost = (int)round((float)curl_getinfo($ch, CURLINFO_TOTAL_TIME) * 1000);
            $err = curl_error($ch);
            curl_multi_remove_handle($mh, $ch);
            curl_close($ch);
            if ($err !== '' || $resp === false || $resp === null) {
                $results[$i] = ['error' => "HTTP请求失败: $err", '_cost_ms' => $cost];
                continue;
            }
            try {
                $results[$i] = $item['provider']->parseResponse($item['action'], $code, $resp) + ['_cost_ms' => $cost];
            } catch (\Throwable $e) {
                $dbg = $item['action'] . ' params=' . json_encode($taskParams[$i] ?? [], JSON_UNESCAPED_UNICODE);
                $results[$i] = ['error' => $e->getMessage() . ' [' . $dbg . ']', '_cost_ms' => $cost];
            }
        }
        curl_multi_close($mh);
        ksort($results);
        return array_values($results);
    }
}

class ProviderException extends \RuntimeException {}
