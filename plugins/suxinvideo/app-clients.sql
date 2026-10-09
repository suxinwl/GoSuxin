-- Native client pairing, releases and temporary cast jobs. Business data is preserved.

CREATE TABLE IF NOT EXISTS sx_app_pairing (
 id CHAR(48) PRIMARY KEY,code CHAR(6) NOT NULL,poll_hash CHAR(64) NOT NULL,
 device_id VARCHAR(120) NOT NULL,name VARCHAR(120) NOT NULL,user_id INT UNSIGNED NOT NULL DEFAULT 0,
 status VARCHAR(20) NOT NULL DEFAULT 'pending',expires BIGINT NOT NULL,created BIGINT NOT NULL,
 UNIQUE KEY pair_code(code),KEY expires(expires)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_app_paired_device (
 user_id INT UNSIGNED NOT NULL,device_id VARCHAR(120) NOT NULL,name VARCHAR(120) NOT NULL,
 last_seen BIGINT NOT NULL,revoked TINYINT NOT NULL DEFAULT 0,PRIMARY KEY(user_id,device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_app_cast_session (
 id CHAR(48) PRIMARY KEY,user_id INT UNSIGNED NOT NULL,mobile_device VARCHAR(120) NOT NULL,
 tv_device VARCHAR(120) NOT NULL,expires BIGINT NOT NULL,created BIGINT NOT NULL,
 KEY tv(user_id,tv_device,expires)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_app_release (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,platform VARCHAR(12) NOT NULL,
 version_name VARCHAR(40) NOT NULL,version_code BIGINT NOT NULL,min_sdk INT NOT NULL DEFAULT 23,
 filename VARCHAR(200) NOT NULL,package_name VARCHAR(120) NOT NULL,signer_sha256 CHAR(64) NOT NULL,
 size BIGINT NOT NULL,sha256 CHAR(64) NOT NULL,changelog TEXT NOT NULL,status TINYINT NOT NULL DEFAULT 1,
 created BIGINT NOT NULL,UNIQUE KEY variant_version(platform,version_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_app_cast_job (
 id CHAR(48) PRIMARY KEY,user_id INT UNSIGNED NOT NULL,device_id VARCHAR(120) NOT NULL,
 vod_id INT UNSIGNED NOT NULL,status VARCHAR(20) NOT NULL,progress INT NOT NULL DEFAULT 0,
 position_ms BIGINT NOT NULL DEFAULT 0,duration_ms BIGINT NOT NULL DEFAULT 0,size BIGINT NOT NULL DEFAULT 0,
 error VARCHAR(200) NOT NULL DEFAULT '',created BIGINT NOT NULL,expires BIGINT NOT NULL,
 KEY owner(user_id,device_id,created),KEY expires(expires)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
