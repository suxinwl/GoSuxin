-- SuxinVideo CMS schema. Business data is preserved on reinstall/uninstall.
SET NAMES utf8mb4;

-- Verified duplicate groups retain their original sx_vod IDs and records.
CREATE TABLE IF NOT EXISTS `sx_vod_alias` (
 vod_id INT UNSIGNED NOT NULL,
 canonical_id INT UNSIGNED NOT NULL,
 reason VARCHAR(255) NOT NULL DEFAULT '',
 created INT UNSIGNED NOT NULL DEFAULT 0,
 updatetime INT UNSIGNED NOT NULL DEFAULT 0,
 PRIMARY KEY (vod_id), KEY canonical_id (canonical_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_source_discovery` (
 vod_id BIGINT NOT NULL PRIMARY KEY, status VARCHAR(16) NOT NULL DEFAULT 'done',
 run_token CHAR(32) NOT NULL DEFAULT '', collector_fingerprint CHAR(64) NOT NULL DEFAULT '',
 lease_until BIGINT NOT NULL DEFAULT 0, next_check BIGINT NOT NULL DEFAULT 0,
 checked INT NOT NULL DEFAULT 0, total INT NOT NULL DEFAULT 0,
 added INT NOT NULL DEFAULT 0, updated INT NOT NULL DEFAULT 0, failed INT NOT NULL DEFAULT 0,
 message VARCHAR(250) NOT NULL DEFAULT '', started_at BIGINT NOT NULL DEFAULT 0,
 updated_at BIGINT NOT NULL DEFAULT 0, KEY discovery_lease (lease_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_vod_source_health` (
  vod_id BIGINT NOT NULL, fingerprint CHAR(64) NOT NULL,
  source_code VARCHAR(200) NOT NULL DEFAULT '', fail_rounds INT NOT NULL DEFAULT 0,
  last_check BIGINT NOT NULL DEFAULT 0, checking_until BIGINT NOT NULL DEFAULT 0,
  hidden_until BIGINT NOT NULL DEFAULT 0, last_error VARCHAR(250) NOT NULL DEFAULT '',
  updated_at BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (vod_id,fingerprint), KEY hidden_until (hidden_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_admin_log` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `admin_id` bigint unsigned NOT NULL,
  `action` varchar(200) NOT NULL,
  `ip` varchar(64) NOT NULL DEFAULT '',
  `created` int unsigned NOT NULL,
  PRIMARY KEY (`id`),
  KEY `created` (`created`),
  KEY `admin_id` (`admin_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_article` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `title` varchar(200) NOT NULL,
  `content` mediumtext,
  `status` tinyint NOT NULL DEFAULT '1',
  `addtime` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_collect_api` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(60) NOT NULL,
  `api_url` varchar(300) NOT NULL,
  `remark` varchar(200) DEFAULT '',
  `status` tinyint NOT NULL DEFAULT '1',
  `collect_auto` tinyint NOT NULL DEFAULT '0',
  `collect_hours` int NOT NULL DEFAULT '12',
  `addtime` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_comment` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `user_id` int unsigned NOT NULL,
  `vod_id` int unsigned NOT NULL,
  `content` varchar(500) NOT NULL,
  `status` tinyint NOT NULL DEFAULT '1',
  `created` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `vod_id` (`vod_id`),
  KEY `user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_config` (
  `key` varchar(64) NOT NULL,
  `value` text,
  PRIMARY KEY (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_email_code` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `email` varchar(120) NOT NULL,
  `code` varchar(10) NOT NULL,
  `type` varchar(20) NOT NULL DEFAULT 'register',
  `expire` int unsigned NOT NULL DEFAULT '0',
  `used` tinyint NOT NULL DEFAULT '0',
  `created` int unsigned NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `email` (`email`,`type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_fav` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `user_id` int unsigned NOT NULL,
  `vod_id` int unsigned NOT NULL,
  `created` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uv` (`user_id`,`vod_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_film_request` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `user_id` int unsigned NOT NULL,
  `title` varchar(120) NOT NULL,
  `note` varchar(500) DEFAULT '',
  `status` tinyint NOT NULL DEFAULT '0',
  `created` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `user_id` (`user_id`),
  KEY `status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_goods` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(60) NOT NULL,
  `price` decimal(10,2) NOT NULL DEFAULT '0.00',
  `points` int NOT NULL DEFAULT '0',
  `days` int NOT NULL DEFAULT '0',
  `sort` int NOT NULL DEFAULT '0',
  `status` tinyint NOT NULL DEFAULT '1',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_link` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(60) NOT NULL,
  `url` varchar(300) NOT NULL,
  `sort` int NOT NULL DEFAULT '0',
  `status` tinyint NOT NULL DEFAULT '1',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_login_fail` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `type` varchar(10) NOT NULL,
  `account` varchar(120) NOT NULL,
  `ip` varchar(64) NOT NULL,
  `fails` int NOT NULL DEFAULT '0',
  `ban_until` int unsigned NOT NULL DEFAULT '0',
  `updated_at` int unsigned NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `tk` (`type`,`account`,`ip`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_order` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `order_no` varchar(30) NOT NULL,
  `user_id` int unsigned NOT NULL,
  `goods_id` int unsigned NOT NULL DEFAULT '0',
  `type` varchar(10) NOT NULL DEFAULT 'points',
  `title` varchar(120) DEFAULT '',
  `amount` decimal(10,2) NOT NULL DEFAULT '0.00',
  `usdt_amount` decimal(12,2) DEFAULT NULL,
  `pay_type` varchar(20) DEFAULT '',
  `status` tinyint NOT NULL DEFAULT '0',
  `trade_no` varchar(64) DEFAULT '',
  `created` int unsigned DEFAULT '0',
  `paid_time` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `order_no` (`order_no`),
  KEY `user_id` (`user_id`),
  KEY `status` (`status`,`created`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_play_record` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `user_id` int unsigned NOT NULL,
  `vod_id` int unsigned NOT NULL,
  `episode` int NOT NULL DEFAULT '1',
  `position` int unsigned NOT NULL DEFAULT '0',
  `source_code` varchar(100) NOT NULL DEFAULT '',
  `episode_key` varchar(200) NOT NULL DEFAULT '',
  `version_key` varchar(100) NOT NULL DEFAULT '',
  `duration_ms` bigint NOT NULL DEFAULT '0',
  `updated` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uv` (`user_id`,`vod_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_player` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `code` varchar(30) NOT NULL,
  `name` varchar(60) DEFAULT '',
  `parse` varchar(300) DEFAULT '',
  `status` tinyint NOT NULL DEFAULT '1',
  PRIMARY KEY (`id`),
  UNIQUE KEY `code` (`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_plugin` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `code` varchar(60) NOT NULL,
  `name` varchar(60) DEFAULT '',
  `type` varchar(10) NOT NULL DEFAULT 'plugin',
  `version` varchar(20) DEFAULT '1.0',
  `author` varchar(60) DEFAULT '',
  `status` tinyint NOT NULL DEFAULT '0',
  `expire` int unsigned NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `code` (`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_sign` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `user_id` int unsigned NOT NULL,
  `day` int unsigned NOT NULL,
  `points` int NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `ud` (`user_id`,`day`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_collect_job` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `mode` varchar(16) NOT NULL,
  `status` varchar(16) NOT NULL DEFAULT 'running',
  `active_key` tinyint DEFAULT NULL,
  `source_id` bigint NOT NULL DEFAULT '0',
  `source_name` varchar(120) NOT NULL DEFAULT '',
  `source_index` int NOT NULL DEFAULT '0',
  `source_count` int NOT NULL DEFAULT '0',
  `page` int NOT NULL DEFAULT '0',
  `page_count` int NOT NULL DEFAULT '0',
  `item` int NOT NULL DEFAULT '0',
  `item_count` int NOT NULL DEFAULT '0',
  `added` int NOT NULL DEFAULT '0',
  `updated` int NOT NULL DEFAULT '0',
  `failed` int NOT NULL DEFAULT '0',
  `skipped` int NOT NULL DEFAULT '0',
  `error` text,
  `type_id` int NOT NULL DEFAULT '0',
  `hours` int NOT NULL DEFAULT '0',
  `start_page` int NOT NULL DEFAULT '1',
  `one_page` tinyint NOT NULL DEFAULT '0',
  `cancel_requested` tinyint NOT NULL DEFAULT '0',
  `started_at` bigint NOT NULL,
  `updated_at` bigint NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `one_active_job` (`active_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_collect_job_source` (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 job_id BIGINT UNSIGNED NOT NULL, source_id BIGINT NOT NULL,
 source_name VARCHAR(120) NOT NULL DEFAULT '', source_url TEXT NOT NULL,
 source_index INT NOT NULL DEFAULT 1, lease_key CHAR(64) NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'queued',
 start_page INT NOT NULL DEFAULT 1, page INT NOT NULL DEFAULT 1, page_count INT NOT NULL DEFAULT 0,
 item INT NOT NULL DEFAULT 0, item_count INT NOT NULL DEFAULT 0,
 added INT NOT NULL DEFAULT 0, updated INT NOT NULL DEFAULT 0, failed INT NOT NULL DEFAULT 0, skipped INT NOT NULL DEFAULT 0,
 error TEXT, type_id INT NOT NULL DEFAULT 0, hours INT NOT NULL DEFAULT 0,
 one_page TINYINT NOT NULL DEFAULT 0, started_at BIGINT NOT NULL DEFAULT 0, updated_at BIGINT NOT NULL,
 UNIQUE KEY source_lease (lease_key), KEY job_sources (job_id,status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_slide` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(120) NOT NULL,
  `pic` varchar(500) NOT NULL,
  `url` varchar(300) DEFAULT '',
  `pos` varchar(10) NOT NULL DEFAULT 'top',
  `sort` int NOT NULL DEFAULT '0',
  `status` tinyint NOT NULL DEFAULT '1',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_topic` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(60) NOT NULL,
  `pic` varchar(500) DEFAULT '',
  `description` varchar(500) DEFAULT '',
  `content` text,
  `status` tinyint NOT NULL DEFAULT '1',
  `addtime` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_type` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `pid` int unsigned NOT NULL DEFAULT '0',
  `name` varchar(30) NOT NULL,
  `sort` int NOT NULL DEFAULT '0',
  `status` tinyint NOT NULL DEFAULT '1',
  `show_home` tinyint NOT NULL DEFAULT '0',
  `icon` varchar(10) NOT NULL DEFAULT '',
  `image` varchar(255) DEFAULT '',
  PRIMARY KEY (`id`),
  KEY `pid` (`pid`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_user` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `email` varchar(120) NOT NULL,
  `name` varchar(60) DEFAULT '',
  `pwd` varchar(255) NOT NULL,
  `points` int NOT NULL DEFAULT '0',
  `vip_expire` int unsigned DEFAULT '0',
  `avatar` varchar(255) DEFAULT '',
  `status` tinyint NOT NULL DEFAULT '1',
  `reg_ip` varchar(64) DEFAULT '',
  `reg_time` int unsigned DEFAULT '0',
  `email_verified` tinyint NOT NULL DEFAULT '0',
  `last_login_time` int unsigned DEFAULT '0',
  `last_login_ip` varchar(64) DEFAULT '',
  `sign_day` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `email` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_user_vod` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `user_id` int unsigned NOT NULL,
  `vod_id` int unsigned NOT NULL,
  `points` int NOT NULL DEFAULT '0',
  `created` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uv` (`user_id`,`vod_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_vod` (
  `id` int unsigned NOT NULL AUTO_INCREMENT,
  `type_id` int unsigned NOT NULL DEFAULT '0',
  `api_id` int unsigned NOT NULL DEFAULT '0',
  `api_vid` varchar(32) DEFAULT '',
  `name` varchar(120) NOT NULL,
  `name_norm` varchar(130) NOT NULL DEFAULT '',
  `sub` varchar(120) DEFAULT '',
  `class` varchar(200) DEFAULT '',
  `year` varchar(20) DEFAULT '',
  `area` varchar(40) DEFAULT '',
  `lang` varchar(40) DEFAULT '',
  `remarks` varchar(60) DEFAULT '',
  `score` decimal(3,1) NOT NULL DEFAULT '0.0',
  `director` varchar(400) DEFAULT '',
  `actor` varchar(1000) DEFAULT '',
  `content` text,
  `pic` varchar(500) DEFAULT '',
  `play_from` varchar(2048) DEFAULT '',
  `play_url` mediumtext,
  `vip` tinyint NOT NULL DEFAULT '0',
  `points` int NOT NULL DEFAULT '0',
  `total_hits` int unsigned NOT NULL DEFAULT '0',
  `status` tinyint NOT NULL DEFAULT '1',
  `addtime` int unsigned DEFAULT '0',
  `updatetime` int unsigned DEFAULT '0',
  PRIMARY KEY (`id`),
  UNIQUE KEY `api` (`api_id`,`api_vid`),
  KEY `type_id` (`type_id`),
  KEY `addtime` (`addtime`),
  KEY `updatetime` (`updatetime`),
  KEY `name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `sx_vod_source_score` (
  vod_id INT UNSIGNED NOT NULL, api_id INT UNSIGNED NOT NULL,
  api_vid VARCHAR(32) NOT NULL DEFAULT '', score DECIMAL(3,1) NOT NULL DEFAULT 0.0,
  score_field VARCHAR(40) NOT NULL DEFAULT '', updated INT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (vod_id,api_id), KEY api_id (api_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- BEGIN PHP PLATFORM SEED
-- Snapshot of PHP platform data. Reinstall adds missing rows only.
-- Global auto collection stays off until the administrator enables it.
INSERT IGNORE INTO `sx_config` (`key`,`value`) VALUES
('site_name','速信影视CMS'),
('site_template','iqiyi'),
('site_mode','cms'),
('captcha_provider','graph'),
('cdn_mode','0'),
('member_enable','1'),
('register_enable','1'),
('points_pay_rate','10'),
('points_sign','5'),
('search_limit_enable','1'),
('search_limit_times','30'),
('search_limit_window','60'),
('collect_auto_interval','60'),
('collect_dedup_title','1'),
('collect_img_local','0'),
('collect_speed','gentle'),
('collect_auto_enable','0');
INSERT IGNORE INTO `sx_type` (`id`,`pid`,`name`,`sort`,`status`,`show_home`,`icon`,`image`) VALUES
(1,0,'电影',1,1,1,'',''),
(2,0,'剧集',2,1,1,'',''),
(3,0,'动漫',3,1,1,'',''),
(4,0,'综艺',4,1,1,'',''),
(5,0,'纪录片',5,1,1,'',''),
(6,0,'短剧',6,1,1,'',''),
(7,3,'中国动漫',50,1,0,'',''),
(8,4,'大陆综艺',50,1,0,'',''),
(9,1,'剧情片',50,1,0,'',''),
(10,2,'马泰剧',50,1,0,'',''),
(11,4,'日韩综艺',50,1,0,'',''),
(12,2,'欧美剧',50,1,0,'',''),
(13,2,'日剧',50,1,0,'',''),
(14,3,'日韩动漫',50,1,0,'',''),
(15,3,'国产动漫',50,1,0,'',''),
(16,4,'港台综艺',50,1,0,'',''),
(17,2,'日本剧',50,1,0,'',''),
(18,2,'台湾剧',50,1,0,'',''),
(19,2,'泰国剧',50,1,0,'',''),
(20,0,'篮球',50,1,0,'',''),
(21,1,'喜剧片',50,1,0,'',''),
(22,2,'大陆剧',50,1,0,'',''),
(23,2,'韩剧',50,1,0,'',''),
(24,3,'日本动漫',50,1,0,'',''),
(25,2,'内地剧',50,1,0,'',''),
(26,1,'伦理片',50,1,0,'',''),
(27,2,'香港剧',50,1,0,'',''),
(28,6,'现代都市',50,1,0,'',''),
(29,6,'古装仙侠',50,1,0,'',''),
(30,2,'国产剧',50,1,0,'',''),
(31,6,'AI短剧',50,1,0,'',''),
(32,1,'恐怖片',50,1,0,'',''),
(33,2,'海外剧',50,1,0,'',''),
(34,2,'其他剧',50,1,0,'',''),
(35,2,'港澳剧',50,1,0,'',''),
(36,1,'爱情片',50,1,0,'','');
INSERT IGNORE INTO `sx_slide` (`id`,`name`,`pic`,`url`,`pos`,`sort`,`status`) VALUES
(1,'万千好片 · 尽在速信影视','/suxinvideo/asset?theme=suxinlite&file=banner.svg','/suxinvideo/','top',0,1),
(2,'新片首发 · 抢先看','/suxinvideo/asset?theme=suxinlite&file=banner2.svg','/suxinvideo/type?id=1','top',0,1),
(3,'VIP专享 · 蓝光画质','/suxinvideo/asset?theme=suxinlite&file=banner3.svg','/suxinvideo/type?id=2','top',0,1),
(4,'每日更新 · 全网聚合','/suxinvideo/asset?theme=suxinlite&file=banner4.svg','/suxinvideo/type?id=3','top',0,1),
(5,'多端畅看 · 随心所欲','/suxinvideo/asset?theme=suxinlite&file=banner5.svg','/suxinvideo/type?id=4','top',0,1);
INSERT IGNORE INTO `sx_collect_api` (`id`,`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`) VALUES
(1,'极速资源','https://jszyapi.com/api.php/provide/vod/at/json','极速云/极速m3u8 官方:jisuzy.tv',1,1,12,1790149021),
(2,'猫眼资源','https://api.maoyanapi.top/api.php/provide/vod/from/mym3u8/at/json','猫眼m3u8线路 官方:maoyanzy.com',1,1,12,1790149021),
(3,'非凡资源','https://api.ffzyapi.com/api.php/provide/vod/from/ffm3u8/at/json','非凡m3u8线路 官方:ffzy.tv',1,1,12,1790149021),
(4,'豆瓣资源','https://caiji.dbzy5.com/api.php/provide/vod/from/dbm3u8/at/json','豆瓣m3u8线路 官方:dbzy.tv',1,1,12,1790149021),
(5,'百度资源','https://api.apibdzy.com/api.php/provide/vod/from/dbm3u8/at/json','百度m3u8线路 官方:bdzy1.com(需官方加白名单)',1,1,12,1790149021);
-- Additional providers from the local Guoguo library. These are opt-in for
-- scheduled collection; reinstallation never changes existing source rows.
INSERT INTO `sx_collect_api` (`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`)
SELECT '红果短剧','hongguo://app','红果 App：真人剧、漫剧、AI剧；播放时解析源站媒体',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM `sx_collect_api` WHERE `api_url`='hongguo://app');
INSERT INTO `sx_collect_api` (`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`)
SELECT '4KVM','4kvm://site','4KVM 站点：电影、电视剧、动漫、综艺',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM `sx_collect_api` WHERE `api_url`='4kvm://site');
INSERT INTO `sx_collect_api` (`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`)
SELECT '量子资源','https://cj.lziapi.com/api.php/provide/vod/','来自果果剧库的苹果 CMS 片源',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM `sx_collect_api` WHERE `api_url` IN ('http://cj.lziapi.com/api.php/provide/vod/','https://cj.lziapi.com/api.php/provide/vod/'));
INSERT INTO `sx_collect_api` (`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`)
SELECT '无极资源','https://api.wujinapi.me/api.php/provide/vod/','来自果果剧库的苹果 CMS 片源',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM `sx_collect_api` WHERE `api_url`='https://api.wujinapi.me/api.php/provide/vod/');
INSERT INTO `sx_collect_api` (`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`)
SELECT '暴风资源','https://bfzyapi.com/api.php/provide/vod/','来自果果剧库的苹果 CMS 片源',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM `sx_collect_api` WHERE `api_url`='https://bfzyapi.com/api.php/provide/vod/');
INSERT INTO `sx_collect_api` (`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`)
SELECT '红牛资源','https://www.hongniuzy2.com/api.php/provide/vod/','来自果果剧库的苹果 CMS 片源',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM `sx_collect_api` WHERE `api_url`='https://www.hongniuzy2.com/api.php/provide/vod/');
INSERT INTO `sx_collect_api` (`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`)
SELECT '闪电资源','https://sdzyapi.com/api.php/provide/vod/','来自果果剧库的苹果 CMS 片源',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM `sx_collect_api` WHERE `api_url`='https://sdzyapi.com/api.php/provide/vod/');
INSERT INTO `sx_collect_api` (`name`,`api_url`,`remark`,`status`,`collect_auto`,`collect_hours`,`addtime`)
SELECT '迅雷资源','https://api.xinlangapi.com/xinlangapi.php/provide//vod/','来自果果剧库的苹果 CMS 片源',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM `sx_collect_api` WHERE `api_url`='https://api.xinlangapi.com/xinlangapi.php/provide//vod/');
INSERT IGNORE INTO `sx_player` (`code`,`name`,`parse`,`status`) VALUES
('no','内置直链播放(mp4/m3u8)','',1),
('jsm3u8','极速m3u8','',1),
('jsyun','极速云','m3u8:{url}/index.m3u8',1),
('mym3u8','猫眼m3u8','',1),
('ffm3u8','非凡m3u8','',1),
('dbm3u8','豆瓣m3u8','',1);
-- END PHP PLATFORM SEED

-- Defaults only. Existing values survive reinstall.
INSERT IGNORE INTO sx_config (`key`,`value`) VALUES
('site_name','速信影视CMS'),('site_template','suxinlite'),('site_domain',''),('site_keywords','速信影视CMS'),('site_description','速信影视CMS'),('site_logo','/suxinvideo/brand?file=app-icon.png'),('site_favicon','/suxinvideo/brand?file=favicon.ico'),('user_default_avatar','/suxinvideo/brand?file=default-avatar.png'),('site_status','1'),('site_register','1');
INSERT IGNORE INTO sx_config (`key`,`value`) VALUES
('source_discovery_enable','1'),('source_discovery_interval','360');
INSERT IGNORE INTO sx_config (`key`,`value`) VALUES
('site_icp',''),('site_mode','cms'),('player_parse',''),('player_autoplay','1'),('player_ad_filter','1'),('player_ad_domains',''),
('member_enable','1'),('register_enable','1'),('comment_audit','0'),('comment_enable','1'),
('points_sign','5'),('points_register','0'),('points_pay_rate','10'),('img_auto_clean_enable','0'),
('kp_icon_search','1'),('kp_icon_history','1'),('kp_icon_user','1'),('kp_slide_enable','1'),
('kp_slide_count','6'),('kp_slide_source','new'),('rewrite_enable','0'),('browser_check_enable','0'),('home_slide_enable','1'),
('captcha_provider','graph'),('geetest_id',''),('geetest_key',''),('turnstile_site_key',''),('turnstile_secret',''),
('smtp_host',''),('smtp_port','465'),('smtp_secure','ssl'),('smtp_user',''),('smtp_pass',''),('smtp_from_name','速信影视CMS'),
('codepay_gateway',''),('codepay_pid',''),('codepay_key',''),('codepay_channel','alipay'),
('epay_gateway',''),('epay_pid',''),('epay_key',''),('epay_channel','alipay'),
('usdt_address',''),('usdt_rate','7'),('usdt_trongrid_key',''),
('wxpay_appid',''),('wxpay_mchid',''),('wxpay_key',''),
('alipay_appid',''),('alipay_private_key',''),('alipay_public_key',''),('cdn_mode','0'),
('ad_home_enable','0'),('ad_home_code',''),('ad_playtop_enable','0'),('ad_playtop_code',''),
('ad_playbottom_enable','0'),('ad_playbottom_code',''),('ad_footer_enable','0'),('ad_footer_code',''),
('debug','0'),('admin_remark',''),('search_limit_enable','1'),('search_limit_times','30'),('search_limit_window','60'),
('baidu_push_site',''),('baidu_push_token',''),('collect_auto_enable','0'),('collect_auto_interval','60'),
('collect_dedup_title','1'),('collect_img_local','0'),('collect_speed','0');
INSERT IGNORE INTO sx_player (code,name,`parse`,status) VALUES
('m3u8','M3U8','',1),('mp4','MP4','',1);
INSERT IGNORE INTO sx_player (code,name,`parse`,status) VALUES
('hongguo','红果短剧','',1),('4kvm','4KVM','',1);
-- 二次元 keeps original remote category IDs. Runtime collection maps them to
-- existing anime children; no local navigation IDs or credentials are baked in.
INSERT IGNORE INTO sx_player (code,name,`parse`,status) VALUES
('ecy_aa02','二次元·极速','',1),('ecy_aa03','二次元·电信','',1),
('ecy_dd02','二次元·有广','',1),('ecy_4k01','二次元·4K','',1);
INSERT INTO sx_collect_api (name,api_url,remark,status,collect_auto,collect_hours,addtime)
SELECT '二次元','erciyuan://app','星次元动漫片源；分类及影片动态读取，分集播放时解析；自动采集各分类近期一页',1,1,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM sx_collect_api WHERE api_url='erciyuan://app');
-- One aggregate source, independent stable player-kind codes. Existing disabled
-- source/player records survive both reinstall and startup upgrades.
INSERT IGNORE INTO sx_config (`key`,`value`) VALUES
('yqk_bootstrap_url','https://59.36.165.33:8976/down/l7e07KwFjWE7.json'),
('yqk_navigation_enable','0'),('home_recommend_source','local'),('home_hero_source','managed');
INSERT IGNORE INTO sx_player (code,name,`parse`,status) VALUES
('yqk_1','小柒APP','',1),('yqk_8','小柒·WJ','',1),('yqk_27','小柒·BF','',1),
('yqk_25','小柒·SN','',1),('yqk_3','小柒·FF','',1),('yqk_5','小柒·SD','',1),
('yqk_12','小柒·LZ','',1),('yqk_24','小柒·YZ','',1),('yqk_38','小柒·HH','',1),
('yqk_37','小柒·JS','',1),('yqk_36','小柒·UK','',1),('yqk_35','小柒·MT','',1),
('yqk_34','小柒·JY','',1),('yqk_33','小柒·KC','',1),('yqk_31','小柒·NN','',1),
('yqk_2','小柒·百度','',1),('yqk_30','小柒·迅雷','',1),('yqk_19','小柒·红牛','',1);
INSERT INTO sx_collect_api (name,api_url,remark,status,collect_auto,collect_hours,addtime)
SELECT '小柒APP聚合资源','yqk://app','动态接口；各线路独立保存，播放时解析清晰度',1,0,12,UNIX_TIMESTAMP()
WHERE NOT EXISTS (SELECT 1 FROM sx_collect_api WHERE api_url='yqk://app');
-- Rename only exact legacy defaults; preserve custom labels and all statuses.
UPDATE sx_player SET name='小柒APP' WHERE code='yqk_1' AND BINARY name='一起看APP';
UPDATE sx_player SET name='小柒·WJ' WHERE code='yqk_8' AND BINARY name='一起看·WJ';
UPDATE sx_player SET name='小柒·BF' WHERE code='yqk_27' AND BINARY name='一起看·BF';
UPDATE sx_player SET name='小柒·SN' WHERE code='yqk_25' AND BINARY name='一起看·SN';
UPDATE sx_player SET name='小柒·FF' WHERE code='yqk_3' AND BINARY name='一起看·FF';
UPDATE sx_player SET name='小柒·SD' WHERE code='yqk_5' AND BINARY name='一起看·SD';
UPDATE sx_player SET name='小柒·LZ' WHERE code='yqk_12' AND BINARY name='一起看·LZ';
UPDATE sx_player SET name='小柒·YZ' WHERE code='yqk_24' AND BINARY name='一起看·YZ';
UPDATE sx_player SET name='小柒·HH' WHERE code='yqk_38' AND BINARY name='一起看·HH';
UPDATE sx_player SET name='小柒·JS' WHERE code='yqk_37' AND BINARY name='一起看·JS';
UPDATE sx_player SET name='小柒·UK' WHERE code='yqk_36' AND BINARY name='一起看·UK';
UPDATE sx_player SET name='小柒·MT' WHERE code='yqk_35' AND BINARY name='一起看·MT';
UPDATE sx_player SET name='小柒·JY' WHERE code='yqk_34' AND BINARY name='一起看·JY';
UPDATE sx_player SET name='小柒·KC' WHERE code='yqk_33' AND BINARY name='一起看·KC';
UPDATE sx_player SET name='小柒·NN' WHERE code='yqk_31' AND BINARY name='一起看·NN';
UPDATE sx_player SET name='小柒·百度' WHERE code='yqk_2' AND BINARY name='一起看·百度';
UPDATE sx_player SET name='小柒·迅雷' WHERE code='yqk_30' AND BINARY name='一起看·迅雷';
UPDATE sx_player SET name='小柒·红牛' WHERE code='yqk_19' AND BINARY name='一起看·红牛';
UPDATE sx_player SET name='小柒APP' WHERE code='yqk_1' AND BINARY name='一起看 APP';
UPDATE sx_collect_api SET name='小柒APP聚合资源' WHERE api_url='yqk://app' AND BINARY name IN ('一起看APP聚合资源','一起看 APP','一起看APP');
INSERT IGNORE INTO sx_plugin (code,name,type,version,author,status,expire) VALUES
('suxinlite','Suxinlite','template','1.0.0','SuxinVideo',1,0),
('suxinpro','SuxinPro','template','1.0.0','SuxinVideo',1,0),
('iqiyi','Iqiyi','template','1.0.0','SuxinVideo',1,0),
('guoguo','果果剧库','template','1.0.0','SuxinVideo',1,0);

-- Global content rules: empty saved values are preserved on reinstall.
INSERT IGNORE INTO sx_config (`key`,`value`) VALUES
('content_block_enable','1'),
('content_block_keywords','擦边\n换妻\n情色\n色情\n成人视频\n成人影片\n三级电影\n三级片'),
('content_block_categories','伦理片\n情色\n色情\n成人影视\n三级电影');

-- Per-provider concurrency shares one service-wide worker limit. Existing choices survive upgrades.
INSERT IGNORE INTO `sx_config` (`key`,`value`) VALUES ('collect_source_concurrency','3');

-- BEGIN NATIVE CLIENT SCHEMA
CREATE TABLE IF NOT EXISTS sx_app_session (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,user_id INT UNSIGNED NOT NULL,
 device_id VARCHAR(120) NOT NULL,device_name VARCHAR(120) NOT NULL DEFAULT '',
 access_hash CHAR(64) NOT NULL,refresh_hash CHAR(64) NOT NULL,
 access_expire BIGINT NOT NULL,refresh_expire BIGINT NOT NULL,revoked TINYINT NOT NULL DEFAULT 0,
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY access_hash(access_hash),
 UNIQUE KEY refresh_hash(refresh_hash),KEY user_device(user_id,device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_app_captcha (
 challenge CHAR(48) NOT NULL PRIMARY KEY,answer_hash CHAR(64) NOT NULL,expire BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_app_license (
 id CHAR(48) NOT NULL PRIMARY KEY,user_id INT UNSIGNED NOT NULL,session_id BIGINT UNSIGNED NOT NULL,
 device_id VARCHAR(120) NOT NULL,vod_id INT UNSIGNED NOT NULL,line VARCHAR(100) NOT NULL,
 episode_key VARCHAR(200) NOT NULL,version_key VARCHAR(100) NOT NULL,quality VARCHAR(120) NOT NULL DEFAULT '',revision CHAR(64) NOT NULL,
 expire BIGINT NOT NULL,revoked TINYINT NOT NULL DEFAULT 0,created BIGINT NOT NULL,
 KEY device(device_id,user_id),KEY vod(vod_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_app_checkout (
 token_hash CHAR(64) NOT NULL PRIMARY KEY,user_id INT UNSIGNED NOT NULL,session_id BIGINT UNSIGNED NOT NULL,
 order_no VARCHAR(30) NOT NULL,payload TEXT NOT NULL,expire BIGINT NOT NULL,
 KEY order_no(order_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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

-- END NATIVE CLIENT SCHEMA

-- BEGIN LIVE PLATFORM SCHEMA
CREATE TABLE IF NOT EXISTS sx_live_group (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,name VARCHAR(120) NOT NULL,
 identity_key CHAR(64) NOT NULL,sort INT NOT NULL DEFAULT 0,enabled TINYINT NOT NULL DEFAULT 1,
 manual_edited TINYINT NOT NULL DEFAULT 0,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY identity_key(identity_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_channel (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,tvg_id VARCHAR(200) NOT NULL DEFAULT '',
 identity_key CHAR(64) NOT NULL,name VARCHAR(200) NOT NULL,logo TEXT NOT NULL,group_id BIGINT UNSIGNED NOT NULL,
 aliases_json VARCHAR(2000) NOT NULL DEFAULT '[]',
 sort INT NOT NULL DEFAULT 0,enabled TINYINT NOT NULL DEFAULT 1,manual_edited TINYINT NOT NULL DEFAULT 0,
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY identity_key(identity_key),KEY group_id(group_id,enabled))
 ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_stream (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,channel_id BIGINT UNSIGNED NOT NULL,
 subscription_id BIGINT UNSIGNED NOT NULL DEFAULT 0,name VARCHAR(200) NOT NULL,url TEXT NOT NULL,url_hash CHAR(64) NOT NULL,
 headers_json TEXT NOT NULL,priority INT NOT NULL DEFAULT 0,quality VARCHAR(100) NOT NULL DEFAULT '',
 enabled TINYINT NOT NULL DEFAULT 1,health VARCHAR(30) NOT NULL DEFAULT 'pending',last_checked BIGINT NOT NULL DEFAULT 0,
 last_error VARCHAR(300) NOT NULL DEFAULT '',manual_edited TINYINT NOT NULL DEFAULT 0,
 source_kind VARCHAR(30) NOT NULL DEFAULT 'url',provider_key VARCHAR(30) NOT NULL DEFAULT '',
 module_key VARCHAR(100) NOT NULL DEFAULT '',provider_ref VARCHAR(500) NOT NULL DEFAULT '',access_level VARCHAR(30) NOT NULL DEFAULT 'public',
 source_revision BIGINT NOT NULL DEFAULT 1,media_type VARCHAR(30) NOT NULL DEFAULT 'hls',epg_id VARCHAR(200) NOT NULL DEFAULT '',catchup_enabled TINYINT NOT NULL DEFAULT 0,
 source_mode VARCHAR(30) NOT NULL DEFAULT 'live',event_id VARCHAR(200) NOT NULL DEFAULT '',duration_ms BIGINT NOT NULL DEFAULT 0,programme_id BIGINT NOT NULL DEFAULT 0,
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY channel_url(channel_id,url_hash),KEY channel_id(channel_id,enabled),
 KEY subscription_id(subscription_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_subscription (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,name VARCHAR(120) NOT NULL,url TEXT NOT NULL,url_hash CHAR(64) NOT NULL,
 enabled TINYINT NOT NULL DEFAULT 1,last_refresh BIGINT NOT NULL DEFAULT 0,last_error VARCHAR(300) NOT NULL DEFAULT '',
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY url_hash(url_hash)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_job (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,status VARCHAR(30) NOT NULL,kind VARCHAR(30) NOT NULL,
 total INT NOT NULL DEFAULT 0,processed INT NOT NULL DEFAULT 0,added INT NOT NULL DEFAULT 0,updated_count INT NOT NULL DEFAULT 0,
 failed INT NOT NULL DEFAULT 0,message VARCHAR(500) NOT NULL DEFAULT '',created BIGINT NOT NULL,updated BIGINT NOT NULL,
 KEY state(status,created)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- END LIVE PLATFORM SCHEMA





-- BEGIN LIVE PROVIDER SCHEMA
CREATE TABLE IF NOT EXISTS sx_live_module (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,provider_key VARCHAR(30) NOT NULL,module_key VARCHAR(100) NOT NULL,
 name VARCHAR(160) NOT NULL DEFAULT '',enabled TINYINT NOT NULL DEFAULT 0,config_json TEXT NOT NULL,secrets_cipher TEXT NOT NULL,
 status VARCHAR(30) NOT NULL DEFAULT 'pending',last_sync BIGINT NOT NULL DEFAULT 0,last_error VARCHAR(300) NOT NULL DEFAULT '',
 source_revision BIGINT NOT NULL DEFAULT 1,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY provider_module(provider_key,module_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_profile (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,name VARCHAR(120) NOT NULL,modules_json TEXT NOT NULL,channel_ids_json TEXT NULL,
 enabled TINYINT NOT NULL DEFAULT 1,created BIGINT NOT NULL,updated BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_member_access (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,member_id BIGINT UNSIGNED NOT NULL,module_key VARCHAR(100) NOT NULL,
 enabled TINYINT NOT NULL DEFAULT 1,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY member_module(member_id,module_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_distribution (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,member_id BIGINT UNSIGNED NOT NULL DEFAULT 0,profile_id BIGINT UNSIGNED NOT NULL,
 name VARCHAR(120) NOT NULL DEFAULT '',token_hash CHAR(64) NOT NULL,enabled TINYINT NOT NULL DEFAULT 1,
 expires_at BIGINT NOT NULL DEFAULT 0,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY token_hash(token_hash),KEY member_id(member_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_programme (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,channel_id BIGINT UNSIGNED NOT NULL,provider_key VARCHAR(30) NOT NULL,
 module_key VARCHAR(100) NOT NULL,provider_ref VARCHAR(500) NOT NULL,epg_id VARCHAR(200) NOT NULL,
 identity_key CHAR(64) NOT NULL,title VARCHAR(300) NOT NULL,description TEXT NOT NULL,
 start_at BIGINT NOT NULL,end_at BIGINT NOT NULL,replay_kind VARCHAR(30) NOT NULL DEFAULT '',
 created BIGINT NOT NULL,updated BIGINT NOT NULL,UNIQUE KEY identity_key(identity_key),KEY channel_time(channel_id,start_at,end_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sx_live_provider_binding (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,provider_key VARCHAR(30) NOT NULL,module_key VARCHAR(100) NOT NULL,
 provider_ref VARCHAR(500) NOT NULL,channel_id BIGINT UNSIGNED NOT NULL,created BIGINT NOT NULL,updated BIGINT NOT NULL,
 UNIQUE KEY provider_ref(provider_key,module_key,provider_ref)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- END LIVE PROVIDER SCHEMA
