<?php
namespace App\Core;

class Logger
{
    public static function op(?int $userId, string $username, string $action, string $target = '', mixed $detail = null): void
    {
        Database::i()->prepare('INSERT INTO operation_logs (user_id,username,action,target,detail,ip) VALUES (?,?,?,?,?,?)')
            ->execute([$userId, $username, $action, $target, is_string($detail) ? $detail : json_encode($detail, JSON_UNESCAPED_UNICODE), Http::ip()]);
    }

    public static function sync(?int $recordId, int $accountId, string $provider, string $action, bool $success, string $message = '', ?int $costMs = null): void
    {
        Database::i()->prepare('INSERT INTO sync_logs (record_id,account_id,provider,action,status,message,request_cost_ms) VALUES (?,?,?,?,?,?,?)')
            ->execute([$recordId, $accountId, $provider, $action, $success ? 'success' : 'failed', mb_substr($message, 0, 500), $costMs]);
    }
}
