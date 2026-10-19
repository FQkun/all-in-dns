<?php
namespace App\Core\Providers;

// 跨平台解析线路映射：统一标准键，再按平台输出各自取值；未登记线路原样透传
class LineMapper
{
    private const OUTPUT = [
        'default' => ['tencent' => '默认',   'aliyun' => 'default',  'volc' => 'default'],
        'telecom' => ['tencent' => '电信',   'aliyun' => 'telecom',  'volc' => 'telecom'],
        'unicom'  => ['tencent' => '联通',   'aliyun' => 'unicom',   'volc' => 'unicom'],
        'mobile'  => ['tencent' => '移动',   'aliyun' => 'mobile',   'volc' => 'mobile'],
        'tietong' => ['tencent' => '铁通',   'aliyun' => 'tietong',  'volc' => 'tietong'],
        'edu'     => ['tencent' => '教育网', 'aliyun' => 'edu',      'volc' => 'edu'],
        'oversea' => ['tencent' => '境外',   'aliyun' => 'oversea',  'volc' => 'oversea'],
    ];

    private const LABEL = [
        'default' => '默认', 'telecom' => '电信', 'unicom' => '联通', 'mobile' => '移动',
        'tietong' => '铁通', 'edu' => '教育网', 'oversea' => '境外',
    ];

    private const ALIAS = [
        '默认' => 'default', 'default' => 'default',
        '电信' => 'telecom', 'telecom' => 'telecom', 'cn_telecom' => 'telecom',
        '联通' => 'unicom', 'unicom' => 'unicom', 'cn_unicom' => 'unicom', 'cucc' => 'unicom',
        '移动' => 'mobile', 'mobile' => 'mobile', 'cn_mobile' => 'mobile', 'cmcc' => 'mobile',
        '铁通' => 'tietong', 'tietong' => 'tietong',
        '教育网' => 'edu', 'edu' => 'edu', 'cernet' => 'edu',
        '境外' => 'oversea', 'oversea' => 'oversea', 'overseas' => 'oversea',
    ];

    private static function key(?string $line): ?string
    {
        $l = trim((string)$line);
        if ($l === '') return 'default';
        return self::ALIAS[strtolower($l)] ?? null;
    }

    /** 转为平台写入值；未登记线路原样透传 */
    public static function to(string $provider, ?string $line): string
    {
        $raw = trim((string)$line);
        $key = self::key($raw);
        if ($key === null) return $raw;
        return self::OUTPUT[$key][$provider] ?? $raw;
    }

    /** 平台值还原为标准中文线路名；未登记线路原样返回 */
    public static function from(string $provider, ?string $line): string
    {
        $raw = trim((string)$line);
        if ($raw === '') return '默认';
        foreach (self::OUTPUT as $key => $byProvider) {
            if (isset($byProvider[$provider]) && $byProvider[$provider] === $raw) return self::LABEL[$key];
        }
        $key = self::key($raw);
        return $key !== null ? self::LABEL[$key] : $raw;
    }
}
