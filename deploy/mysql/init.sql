CREATE TABLE IF NOT EXISTS shipments (
  id VARCHAR(64) PRIMARY KEY,
  route VARCHAR(255) NOT NULL,
  cargo VARCHAR(255) NOT NULL,
  driver VARCHAR(64) NOT NULL DEFAULT '',
  vehicle VARCHAR(64) NOT NULL DEFAULT '',
  eta VARCHAR(32) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_shipments_status_updated (status, updated_at)
);
CREATE TABLE IF NOT EXISTS shipment_events (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  shipment_id VARCHAR(64) NOT NULL,
  from_status VARCHAR(32) NOT NULL,
  to_status VARCHAR(32) NOT NULL,
  actor VARCHAR(64) NOT NULL,
  note VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  KEY idx_shipment_events_shipment (shipment_id, id)
);
CREATE TABLE IF NOT EXISTS drivers (id VARCHAR(32) PRIMARY KEY, name VARCHAR(64) NOT NULL, phone VARCHAR(32) NOT NULL, vehicle VARCHAR(64) NOT NULL, status VARCHAR(32) NOT NULL);
CREATE TABLE IF NOT EXISTS vehicles (id VARCHAR(32) PRIMARY KEY, plate VARCHAR(64) NOT NULL, type VARCHAR(64) NOT NULL, status VARCHAR(32) NOT NULL);
CREATE TABLE IF NOT EXISTS exceptions (id VARCHAR(32) PRIMARY KEY, shipment_id VARCHAR(64) NOT NULL, type VARCHAR(64) NOT NULL, text VARCHAR(255) NOT NULL, level VARCHAR(16) NOT NULL, status VARCHAR(16) NOT NULL, created_at DATETIME(6) NOT NULL, resolved_at DATETIME(6) NULL, KEY idx_exceptions_status_created (status, created_at));
CREATE TABLE IF NOT EXISTS settlements (id VARCHAR(64) PRIMARY KEY, period VARCHAR(64) NOT NULL, status VARCHAR(16) NOT NULL, driver_count INT NOT NULL, shipment_count INT NOT NULL, amount DECIMAL(12,2) NOT NULL);

CREATE TABLE IF NOT EXISTS venues (
  id VARCHAR(32) PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  address VARCHAR(255) NOT NULL,
  capacity INT NOT NULL,
  status VARCHAR(32) NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id VARCHAR(64) PRIMARY KEY,
  venue_id VARCHAR(32) NOT NULL,
  title VARCHAR(160) NOT NULL,
  starts_at DATETIME(6) NOT NULL,
  ends_at DATETIME(6) NOT NULL,
  capacity INT NOT NULL,
  price DECIMAL(12,2) NOT NULL,
  sold INT NOT NULL DEFAULT 0,
  checked_in INT NOT NULL DEFAULT 0,
  pending_exceptions INT NOT NULL DEFAULT 0,
  status VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_sessions_status_starts (status, starts_at),
  KEY idx_sessions_venue_starts (venue_id, starts_at)
);
CREATE TABLE IF NOT EXISTS tickets (
  id VARCHAR(96) PRIMARY KEY,
  session_id VARCHAR(64) NOT NULL,
  code VARCHAR(128) NOT NULL UNIQUE,
  status VARCHAR(32) NOT NULL,
  price DECIMAL(12,2) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  checked_in_at DATETIME(6) NULL,
  KEY idx_tickets_session_status (session_id, status)
);
CREATE TABLE IF NOT EXISTS session_events (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  session_id VARCHAR(64) NOT NULL,
  action VARCHAR(64) NOT NULL,
  from_status VARCHAR(32) NOT NULL DEFAULT '',
  to_status VARCHAR(32) NOT NULL DEFAULT '',
  actor VARCHAR(64) NOT NULL,
  detail VARCHAR(255) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  KEY idx_session_events_session (session_id, id)
);
CREATE TABLE IF NOT EXISTS session_settlements (
  id VARCHAR(96) PRIMARY KEY,
  session_id VARCHAR(64) NOT NULL UNIQUE,
  ticket_count INT NOT NULL,
  gross DECIMAL(12,2) NOT NULL,
  status VARCHAR(32) NOT NULL,
  settled_at DATETIME(6) NOT NULL
);

INSERT IGNORE INTO drivers VALUES
('D-001','周师傅','13800000001','沪A·72K31','现场服务中'),('D-002','陈师傅','13800000002','沪B·18Q90','现场服务中'),('D-003','林师傅','13800000003','沪C·39P06','休息中'),('D-004','王师傅','13800000004','沪D·55L18','现场服务中'),('D-005','赵师傅','13800000005','沪E·03R88','现场服务中'),('D-006','孙师傅','13800000006','沪F·61P72','现场服务中');
INSERT IGNORE INTO vehicles VALUES ('V-001','沪A·72K31','冷链车','在线'),('V-002','沪B·18Q90','厢式货车','在线'),('V-003','沪C·39P06','冷链车','维护'),('V-004','沪D·55L18','厢式货车','在线');
INSERT IGNORE INTO settlements VALUES ('SET-2026-07-01','07/01 - 07/07','已结算',38,386,24680),('SET-2026-07-08','07/08 - 07/14','已结算',42,428,31220),('SET-2026-07-15','07/15 - 07/21','待确认',40,198,12680);
INSERT IGNORE INTO venues VALUES ('VEN-001','云栖会展中心','上海市静安区演示路 18 号',1200,'营业中'),('VEN-002','星河艺术馆','上海市徐汇区演示路 66 号',600,'营业中'),('VEN-003','城市公园草坪','上海市浦东新区演示公园',2000,'营业中');
INSERT IGNORE INTO shipments (id,route,cargo,driver,vehicle,eta,status,created_at,updated_at) VALUES
('FF-260716-018','浦东 → 静安','生鲜 12 箱','周师傅','沪A·72K31','14:35','现场服务中',UTC_TIMESTAMP(),UTC_TIMESTAMP()),
('FF-260716-017','虹桥 → 徐汇','餐饮食材 28 箱','陈师傅','沪B·18Q90','15:10','待接单',UTC_TIMESTAMP(),UTC_TIMESTAMP()),
('FF-260716-016','杨浦 → 宝山','电商包裹 86 件','林师傅','沪C·39P06','已签收','已完成',UTC_TIMESTAMP(),UTC_TIMESTAMP()),
('FF-260716-015','闵行 → 长宁','办公物资 16 箱','王师傅','沪D·55L18','16:20','待调度',UTC_TIMESTAMP(),UTC_TIMESTAMP());
INSERT IGNORE INTO exceptions (id,shipment_id,type,text,level,status,created_at) VALUES ('EX-041','FF-260716-018','超时预警','预计晚到 18 分钟','高','待处理',UTC_TIMESTAMP()),('EX-040','FF-260716-017','地址确认','收货人电话无人接听','中','待处理',UTC_TIMESTAMP()),('EX-039','FF-260716-016','场地资源告警','需要补充冷链温度记录','低','待处理',UTC_TIMESTAMP());
