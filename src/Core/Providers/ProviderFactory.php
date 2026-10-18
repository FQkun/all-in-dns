<?php
namespace App\Core\Providers;

use App\Core\Crypto;
use App\Core\Database;

class ProviderFactory
{
    public static function make(array $account): BaseProvider
    {
        $ak = Crypto::decrypt($account['access_key_id']);
        $sk = Crypto::decrypt($account['access_key_secret']);
        $extra = $account['extra'] ? json_decode($account['extra'], true) ?: [] : [];
        return match ($account['provider']) {
            'tencent' => new TencentProvider($ak, $sk, $extra),
            'aliyun' => new AliyunProvider($ak, $sk, $extra),
            'volc' => new VolcProvider($ak, $sk, $extra),
            default => throw new ProviderException('未知云厂商: ' . $account['provider']),
        };
    }

    public static function makeById(int $accountId): BaseProvider
    {
        $st = Database::i()->prepare('SELECT * FROM cloud_accounts WHERE id=?');
        $st->execute([$accountId]);
        $acc = $st->fetch();
        if (!$acc) throw new ProviderException('云账号不存在: ' . $accountId);
        return self::make($acc);
    }
}
