-- name: PruneOIDCFlows :exec
DELETE FROM oidc_flows WHERE expires_at <= $1;
-- name: DeleteOIDCFlowForPreviousBrowser :exec
DELETE FROM oidc_flows WHERE browser_token_hash=$1;

-- name: InsertOIDCFlow :exec
INSERT INTO oidc_flows(state_hash,browser_token_hash,issuer,client_id,nonce,pkce_verifier,purpose,user_id,session_id,created_at,expires_at,invite_token_hash)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);

-- name: ConsumeOIDCFlow :one
DELETE FROM oidc_flows WHERE state_hash=$1 AND browser_token_hash=$2 AND expires_at>$3 AND issuer=$4 AND client_id=$5
RETURNING issuer,client_id,nonce,pkce_verifier,purpose,COALESCE(user_id::text,'')::text AS user_id,COALESCE(session_id::text,'')::text AS session_id,created_at,expires_at,invite_token_hash;

-- name: PruneOIDCPendingLogins :exec
DELETE FROM oidc_pending_logins WHERE expires_at <= $1;

-- name: FindOIDCCredential :one
SELECT password_hash FROM local_credentials WHERE user_id=$1;

-- name: LinkedOIDCIdentity :one
SELECT EXISTS(SELECT 1 FROM oidc_identities WHERE user_id=$1 AND issuer=$2);
