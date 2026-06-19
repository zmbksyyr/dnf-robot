package sql

import (
	"database/sql"
	"fmt"
)

func CheckDatabase(db *sql.DB) error {
	return db.Ping()
}

func InitializeDatabase(db *sql.DB) error {
	if err := CheckDatabase(db); err != nil {
		return err
	}

	if err := migrateAccountsTable(db); err != nil {
		return err
	}
	if err := migrateStarskyDatabase(db); err != nil {
		return err
	}
	if err := migrateZhanliTable(db); err != nil {
		return err
	}
	if err := migrateNewConfigV4(db); err != nil {
		return err
	}
	if err := migrateNewConfigV5(db); err != nil {
		return err
	}
	if err := migrateGiftTask(db); err != nil {
		return err
	}
	if err := migrateV4Ai(db); err != nil {
		return err
	}
	if err := migrateRobot(db); err != nil {
		return err
	}
	if err := migrateRobotStall(db); err != nil {
		return err
	}
	if err := migrateRobotStallConfig(db); err != nil {
		return err
	}
	if err := migrateTitles(db); err != nil {
		return err
	}
	if err := migrateGiftTaskTables(db); err != nil {
		return err
	}
	if err := migrateMapTask(db); err != nil {
		return err
	}
	if err := migrateRobotAlter(db); err != nil {
		return err
	}
	if err := migrateDummylistAlter(db); err != nil {
		return err
	}
	if err := recreateDataTmp(db); err != nil {
		return err
	}

	return nil
}

func columnExists(db *sql.DB, schemaName, tableName, columnName string) (bool, error) {
	rows, err := Select(db,
		"SELECT TABLE_SCHEMA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? AND COLUMN_NAME=?",
		schemaName, tableName, columnName)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func tableExists(db *sql.DB, schemaName, tableName string) (bool, error) {
	rows, err := Select(db,
		"SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME=?",
		schemaName, tableName)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func migrateAccountsTable(db *sql.DB) error {
	alterations := []struct {
		col string
		def string
	}{
		{"ip", "varchar(64) not null"},
		{"qq", "varchar(64) not null"},
		{"login_IP", "varchar(64) not null"},
		{"login_Mac", "varchar(64) not null"},
		{"seal_IP", "int(11) not null"},
		{"seal_MAC", "int(11) not null"},
		{"seal_accountname", "int(11) not null"},
	}
	for _, a := range alterations {
		exists, err := columnExists(db, "d_taiwan", "accounts", a.col)
		if err != nil {
			return fmt.Errorf("check accounts.%s: %w", a.col, err)
		}
		if !exists {
			sqlStr := fmt.Sprintf("ALTER TABLE d_taiwan.accounts ADD COLUMN %s %s", a.col, a.def)
			if _, err := db.Exec(sqlStr); err != nil {
				return fmt.Errorf("alter accounts.%s: %w", a.col, err)
			}
		}
	}
	return nil
}

func migrateStarskyDatabase(db *sql.DB) error {
	exists, err := tableExists(db, "d_starsky", "cdk")
	if err != nil {
		return fmt.Errorf("check d_starsky: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := db.Exec("CREATE DATABASE IF NOT EXISTS d_starsky DEFAULT CHARACTER SET utf8 DEFAULT COLLATE utf8_general_ci"); err != nil {
		return fmt.Errorf("create d_starsky: %w", err)
	}
	if _, err := db.Exec("USE d_starsky"); err != nil {
		return fmt.Errorf("use d_starsky: %w", err)
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS cdk (
			CID int(11) NOT NULL AUTO_INCREMENT,
			CCDK varchar(36) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			CCode varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			CName varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			CNumber varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			CGold int(11) NOT NULL,
			CGolda int(11) NOT NULL,
			CDGold int(11) NOT NULL,
			qianghua int(11) NOT NULL,
			hongzi int(11) NOT NULL,
			hongzishuzhi int(11) NOT NULL,
			duanzao int(11) NOT NULL,
			chongwu tinyint(1) NOT NULL,
			CState tinyint(1) NOT NULL,
			occ_time varchar(64) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			charac_id int(11) NOT NULL,
			fz varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			cdkduihuanshijian varchar(64) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			cdkduihuanuid int(11) NOT NULL,
			cdkduihuancid int(11) NOT NULL,
			cdkduihuanjiaose varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			yanse varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			leixin varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			PRIMARY KEY (CID) USING BTREE,
			UNIQUE INDEX UNIQUE_CDKEY(CCDK) USING BTREE
		) ENGINE=MyISAM AUTO_INCREMENT=1 CHARACTER SET latin1 COLLATE latin1_swedish_ci ROW_FORMAT=Dynamic`,

		`CREATE TABLE IF NOT EXISTS config (
			YID int(11) NOT NULL auto_increment,
			DBY mediumtext character set utf8 NOT NULL,
			DGJZ mediumtext character set utf8 NOT NULL,
			reward_time varchar(255) character set utf8 NOT NULL,
			Tren varchar(255) character set utf8 NOT NULL,
			ip_reg_control int(11) NOT NULL,
			reg_rum varchar(255) character set utf8 NOT NULL,
			bubbleCount int(11) NOT NULL,
			bubbleType int(11) NOT NULL,
			bubbleTime int(11) NOT NULL,
			register int(11) NOT NULL,
			regnumber int(11) NOT NULL,
			regtype int(11) NOT NULL,
			jsxz int(11) NOT NULL,
			PRIMARY KEY (YID)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`INSERT INTO config (YID, DBY, DGJZ, reward_time, Tren, ip_reg_control, reg_rum, bubbleCount, bubbleType, bubbleTime, register, regnumber, regtype, jsxz)
		VALUES (1, '', '0', '', '0', 1, '0', 5, 2, 60, 0, 0, 0, 0)`,

		`CREATE TABLE IF NOT EXISTS hang_list (
			ID int(11) unsigned NOT NULL auto_increment,
			UID int(11) NOT NULL,
			AccountName varchar(255) NOT NULL,
			CharacterName varchar(255) NOT NULL,
			Reason varchar(1024) NOT NULL,
			Time varchar(1024) NOT NULL,
			PRIMARY KEY (ID)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=utf8 ROW_FORMAT=DYNAMIC`,

		`CREATE TABLE IF NOT EXISTS qfillegal (
			CID int(11) NOT NULL auto_increment,
			IUID varchar(255) character set utf8 NOT NULL,
			IUsername varchar(255) character set utf8 NOT NULL,
			IFile_md5 varchar(255) character set utf8 NOT NULL,
			IMac_md5 varchar(255) character set utf8 NOT NULL,
			MING varchar(255) character set utf8 NOT NULL,
			IKey_word varchar(255) character set utf8 NOT NULL,
			IIPAddress varchar(255) NOT NULL,
			ITime varchar(1024) NOT NULL,
			PRIMARY KEY (CID)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1 ROW_FORMAT=DYNAMIC`,

		`DROP TABLE IF EXISTS dataTmp`,

		`CREATE TABLE dataTmp (
			dataObj mediumblob NULL
		) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

		`DROP TABLE IF EXISTS zhanli`,

		`CREATE TABLE zhanli (
			ZID int(11) NOT NULL auto_increment,
			ZName varchar(255) character set utf8 NOT NULL,
			ZLZ int(11) NOT NULL,
			ZDJ int(11) NOT NULL,
			CID int(11) NOT NULL,
			shield int(11) NOT NULL,
			PRIMARY KEY (ZID)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS Dummylist (
			ID int(11) NOT NULL auto_increment,
			YID varchar(255) character set utf8 NOT NULL,
			UID varchar(255) character set utf8 NOT NULL,
			port varchar(255) character set utf8 NOT NULL,
			curvill varchar(255) character set utf8 NOT NULL,
			curarea varchar(255) character set utf8 NOT NULL,
			curx varchar(255) character set utf8 NOT NULL,
			cury varchar(255) character set utf8 NOT NULL,
			CID varchar(255) character set utf8 NOT NULL,
			ip varchar(100) character set utf8 default NULL,
			function_type int(11) default NULL,
			discost int(11) default NULL,
			PRIMARY KEY (ID)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS gift_task (
			task_id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			task_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			task_type int(10) NOT NULL,
			region_id int(10) NOT NULL,
			room_id int(10) NOT NULL,
			user_num int(10) NOT NULL,
			code_num int(10) NOT NULL,
			task_state int(10) NOT NULL,
			gift_odds int(10) NOT NULL,
			gift_type int(10) NOT NULL,
			task_time int(10) NOT NULL,
			task_last_time int(15) NOT NULL,
			name2 varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			PRIMARY KEY USING BTREE (task_id)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS gift_history (
			id int(15) NOT NULL AUTO_INCREMENT,
			task_id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			create_time varchar(50) CHARACTER SET utf8 COLLATE utf8_general_ci NULL DEFAULT NULL,
			content varchar(3000) CHARACTER SET utf8 COLLATE utf8_general_ci NULL DEFAULT NULL,
			PRIMARY KEY USING BTREE (id)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS gift_code (
			id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			task_id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			code_id int(15) NOT NULL,
			code_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			code_num int(10) NOT NULL,
			code_odds int(10) NOT NULL,
			code_state int(10) NOT NULL,
			PRIMARY KEY USING BTREE (id)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS gift_user (
			cid int(11) NOT NULL,
			region int(10) NULL DEFAULT NULL,
			room int(10) NULL DEFAULT NULL,
			PRIMARY KEY USING BTREE (cid)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS map_task (
			task_id int(11) NOT NULL AUTO_INCREMENT,
			task_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			map_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			reward_state int(10) NOT NULL,
			msg_state int(10) NOT NULL,
			msg_text varchar(500) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			task_state int(10) NOT NULL,
			msg_single_text varchar(500) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			is_steam_reward int(10) NOT NULL,
			PRIMARY KEY USING BTREE (task_id)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS map_history (
			ID int(11) UNSIGNED NOT NULL AUTO_INCREMENT,
			uid int(11) NOT NULL,
			charactName varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			dungeonId int(11) NOT NULL,
			dungeonName varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			difficulty int(11) NOT NULL,
			mapCount int(11) NOT NULL,
			killMobCount int(11) NOT NULL,
			useCubepieceClearCount int(11) NOT NULL,
			hell int(11) NOT NULL,
			passingTime int(11) NOT NULL,
			clearInfo int(11) NOT NULL,
			success int(11) NOT NULL,
			leaveTime datetime NOT NULL,
			reward_state int(11) NOT NULL,
			is_captain int(11) NOT NULL,
			is_team int(11) NOT NULL,
			c_id int(11) NOT NULL,
			msg_text varchar(500) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			PRIMARY KEY USING BTREE (ID)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS map_code (
			id int(10) NOT NULL AUTO_INCREMENT,
			task_id int(10) NOT NULL,
			code_id int(15) NOT NULL,
			code_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			code_num int(10) NOT NULL,
			code_odds int(10) NOT NULL,
			code_state int(10) NOT NULL,
			PRIMARY KEY USING BTREE (id)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS titles (
			seal_ip int(11) UNSIGNED NOT NULL DEFAULT 1,
			seal_mac int(11) UNSIGNED NOT NULL DEFAULT 1,
			seal_uid int(11) UNSIGNED NOT NULL DEFAULT 1,
			seal_announcement int(11) UNSIGNED NOT NULL DEFAULT 1,
			seal_content varchar(500) CHARACTER SET utf8 COLLATE utf8_general_ci NULL DEFAULT NULL
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`CREATE TABLE IF NOT EXISTS Robot (
			robot int(11) NOT NULL auto_increment,
			aiState varchar(255) character set utf8 NOT NULL,
			speed varchar(255) character set utf8 NOT NULL,
			PRIMARY KEY (robot)
		) ENGINE=MyISAM AUTO_INCREMENT=1 DEFAULT CHARSET=latin1`,

		`INSERT INTO Robot (robot, aiState, speed) VALUES (1, '1', '1')`,

		`INSERT INTO titles (seal_ip, seal_mac, seal_uid, seal_announcement, seal_content)
		VALUES (1, 1, 1, 1, '255##5##玩家%s使用非法程序，已封号处理！请不要使用第三方程序')`,

		`CREATE TABLE IF NOT EXISTS new_config (
			ID int(11) NOT NULL AUTO_INCREMENT,
			coordinate mediumtext character set utf8 NOT NULL,
			NameofLander mediumtext character set utf8 NOT NULL,
			Gameannouncement varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Grade mediumtext character set utf8 NOT NULL,
			Materialcolor varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			switch varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			levelmax varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			zlbs varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			NPCcolor varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Charactercolor varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			PVF_md5 varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			PVFverification varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			CDKcontrol varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Communication varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Officialwebsite varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Onlineupdate varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Recharge varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Updatetype varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Virtual varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			fgkg varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			customerQQ varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			luckdraw varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			Hotkeys varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			switch_regCdkey int(11) NOT NULL,
			switch_reCdkey int(11) NOT NULL,
			reDCdkeyOpt varchar(4096) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			douxing1 varchar(1024) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			douxing2 varchar(1024) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			douxing3 varchar(1024) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			douxing4 varchar(1024) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			douxing5 varchar(1024) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			douxing6 varchar(1024) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			v4_diaoluo_texiao mediumtext character set utf8 NOT NULL,
			v4_ani_texiao mediumtext character set utf8 NOT NULL,
			PRIMARY KEY (ID) USING BTREE
		) ENGINE=InnoDB AUTO_INCREMENT=1 CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

		`INSERT INTO new_config (ID, coordinate, NameofLander, Gameannouncement, Grade, Materialcolor, switch, levelmax, zlbs, Name, NPCcolor, Charactercolor, PVF_md5, PVFverification, CDKcontrol, Communication, Officialwebsite, Onlineupdate, Recharge, Updatetype, Virtual, fgkg, customerQQ, luckdraw, Hotkeys, switch_regCdkey, switch_reCdkey, reDCdkeyOpt, douxing1, douxing2, douxing3, douxing4, douxing5, douxing6, v4_diaoluo_texiao, v4_ani_texiao)
		VALUES (1, '', '', '欢迎进入游戏！', '', '68D5ED*68D5ED*68D5ED*68D5ED*68D5ED*68D5ED*68D5ED', '0*1*1*1*1*1*0*0*1*1*1*1*1*1*1*1*1*0*0*1*0*0*0*1*0*1*1*1*1*1*0*1*0*1*1*1*1*1*1*1*1*1*1*1*1*1*1*0*1*0*4*', '70', '10***10***10***10***15***2***', 'DNF登录器', 'FFDC64***FFDC64***FFDC64***00FF00***FF80C0***68D5ED***FF00F0***68D5ED', 'FFDC64***FF0000***FF8000***00FF00***0080FF***0080FF', '0v*vScript.pvfv*v0', '0', '1*255', 'http://www.baidu.com', 'http://www.baidu.com', '0', 'http://www.baidu.com', '1', '0', '1@@@0@@@5', '123456', 'http://www.baidu.com', '', '', '0', '0', '', '', '', '', '', '', '', '')`,

		`CREATE TABLE IF NOT EXISTS regCdkey (
			ID int(11) UNSIGNED NOT NULL AUTO_INCREMENT,
			Cdkey varchar(64) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			accountname varchar(64) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
			State int(11) NOT NULL,
			PRIMARY KEY(ID) USING BTREE
		) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,
	}

	for i, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("stmt %d: %w", i, err)
		}
	}

	if _, err := db.Exec(`UPDATE new_config SET Charactercolor='FFDC64***FF0000***FF8000***00FF00***0080FF***0080FF',switch='0*1*1*1*1*1*0*0*1*1*1*1*1*1*1*1*1*0*0*1*0*0*0*1*0*1*1*1*1*1*0*1*0*1*1*1*1*1*1*1*1*1*1*1*1*1*1*0*1*0*4*',zlbs='10***10***10***10***15***10***',NPCcolor='FFDC64***FFDC64***FFDC64***00FF00***FF80C0***68D5ED***FF00F0***68D5ED***4278223103***4278255360***FFDC64***1'`); err != nil {
		return fmt.Errorf("update new_config: %w", err)
	}
	if _, err := db.Exec("UPDATE new_config SET v4_ani_texiao='1KHOJK普通0---&&1KHOJK高级0---&&1KHOJK稀有0---&&1KHOJK神器0---&&1KHOJK史诗0---&&1KHOJK勇者0++0'"); err != nil {
		return fmt.Errorf("update new_config v4_ani_texiao: %w", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS Robot (
		robot int(11) UNSIGNED NOT NULL DEFAULT 1,
		aiState int(11) UNSIGNED NOT NULL DEFAULT 1,
		speed int(11) UNSIGNED NOT NULL DEFAULT 100
	) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=COMPACT`); err != nil {
		return fmt.Errorf("create Robot (InnoDB): %w", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS dungeonCheck (
		ID int(11) UNSIGNED NOT NULL AUTO_INCREMENT,
		uid int(11) NOT NULL,
		charactName varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
		dungeonId int(11) NOT NULL,
		dungeonName varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
		difficulty int(11) NOT NULL,
		mapCount int(11) NOT NULL,
		killMobCount int(11) NOT NULL,
		useCubepieceClearCount int(11) NOT NULL,
		hell int(11) NOT NULL,
		passingTime int(11) NOT NULL,
		clearInfo int(11) NOT NULL,
		success int(11) NOT NULL,
		leaveTime datetime NOT NULL,
		PRIMARY KEY(ID) USING BTREE
	) ENGINE=InnoDB AUTO_INCREMENT=1 CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`); err != nil {
		return fmt.Errorf("create dungeonCheck: %w", err)
	}

	return nil
}

func migrateZhanliTable(db *sql.DB) error {
	exists, err := columnExists(db, "d_starsky", "zhanli", "CID")
	if err != nil {
		return fmt.Errorf("check zhanli.CID: %w", err)
	}
	if !exists {
		if _, err := db.Exec("ALTER TABLE d_starsky.zhanli ADD COLUMN CID int(11) NOT NULL"); err != nil {
			return fmt.Errorf("alter zhanli.CID: %w", err)
		}
	}
	return nil
}

func migrateNewConfigV4(db *sql.DB) error {
	exists, err := columnExists(db, "d_starsky", "new_config", "v4_diaoluo_texiao")
	if err != nil {
		return fmt.Errorf("check new_config.v4_diaoluo_texiao: %w", err)
	}
	if !exists {
		if _, err := db.Exec("ALTER TABLE d_starsky.new_config ADD COLUMN v4_diaoluo_texiao mediumtext NOT NULL"); err != nil {
			return fmt.Errorf("add v4_diaoluo_texiao: %w", err)
		}
		if _, err := db.Exec("ALTER TABLE d_starsky.new_config ADD COLUMN v4_ani_texiao mediumtext NOT NULL"); err != nil {
			return fmt.Errorf("add v4_ani_texiao: %w", err)
		}
	}
	return nil
}

func migrateNewConfigV5(db *sql.DB) error {
	exists, err := columnExists(db, "d_starsky", "new_config", "is_open_up_pwd")
	if err != nil {
		return fmt.Errorf("check new_config.is_open_up_pwd: %w", err)
	}
	if !exists {
		alterations := []string{
			"ALTER TABLE d_starsky.new_config ADD COLUMN is_open_up_pwd int(11) default NULL",
			"ALTER TABLE d_starsky.new_config ADD COLUMN txt_temp_1 mediumtext default NULL",
			"ALTER TABLE d_starsky.new_config ADD COLUMN txt_temp_2 mediumtext default NULL",
			"ALTER TABLE d_starsky.new_config ADD COLUMN txt_temp_3 mediumtext default NULL",
			"ALTER TABLE d_starsky.new_config ADD COLUMN txt_temp_4 mediumtext default NULL",
			"ALTER TABLE d_starsky.new_config ADD COLUMN txt_temp_5 mediumtext default NULL",
		}
		for _, sqlStr := range alterations {
			if _, err := db.Exec(sqlStr); err != nil {
				return fmt.Errorf("%s: %w", sqlStr, err)
			}
		}
		if _, err := db.Exec("UPDATE d_starsky.new_config SET is_open_up_pwd=1, txt_temp_1='0', txt_temp_2='0', txt_temp_3='0', txt_temp_4='0', txt_temp_5='0'"); err != nil {
			return fmt.Errorf("update new_config defaults: %w", err)
		}
	}
	return nil
}

func migrateGiftTask(db *sql.DB) error {
	exists, err := columnExists(db, "d_starsky", "gift_task", "time_check_type")
	if err != nil {
		return fmt.Errorf("check gift_task.time_check_type: %w", err)
	}
	if !exists {
		if _, err := db.Exec("ALTER TABLE d_starsky.gift_task ADD COLUMN time_check_type int(11) NOT NULL"); err != nil {
			return fmt.Errorf("add time_check_type: %w", err)
		}
		if _, err := db.Exec("ALTER TABLE d_starsky.gift_task ADD COLUMN time_check varchar(255) NOT NULL"); err != nil {
			return fmt.Errorf("add time_check: %w", err)
		}
	}
	return nil
}

func migrateV4Ai(db *sql.DB) error {
	exists, err := tableExists(db, "d_starsky", "v4_ai")
	if err != nil {
		return fmt.Errorf("check v4_ai: %w", err)
	}
	if !exists {
		if _, err := db.Exec("USE d_starsky"); err != nil {
			return fmt.Errorf("use d_starsky: %w", err)
		}
		v4Statements := []string{
			`CREATE TABLE v4_ai (
				id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				ai_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				ai_msg int(11) NULL DEFAULT NULL,
				ai_zhanjie int(11) NULL DEFAULT NULL,
				ai_state int(11) NULL DEFAULT NULL,
				ai_yisu int(11) NULL DEFAULT NULL,
				ai_sleep int(11) NULL DEFAULT NULL,
				PRIMARY KEY USING BTREE(id)
			) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

			`INSERT INTO v4_ai (id, ai_name, ai_msg, ai_zhanjie, ai_state, ai_yisu, ai_sleep) VALUES ('1', '1', 2, 2, 1, 1, 5)`,

			`CREATE TABLE v4_ai_hh (
				id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				hh_text varchar(2000) CHARACTER SET utf8 COLLATE utf8_general_ci NULL DEFAULT NULL,
				hh_state int(11) NULL DEFAULT NULL,
				PRIMARY KEY USING BTREE(id)
			) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

			`CREATE TABLE v4_ai_map (
				id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				ditu_id int(11) NULL DEFAULT NULL,
				fangjian_id int(11) NULL DEFAULT NULL,
				x_min int(11) NULL DEFAULT NULL,
				x_max int(11) NULL DEFAULT NULL,
				y_min int(11) NULL DEFAULT NULL,
				y_max int(11) NULL DEFAULT NULL,
				ditu_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NULL DEFAULT NULL,
				fangjian_max_time int(11) NULL DEFAULT NULL,
				ditu_state int(11) NULL DEFAULT NULL,
				PRIMARY KEY USING BTREE(id)
			) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

			`CREATE TABLE v4_ai_user (
				uid int(11) NOT NULL,
				msg_state int(11) NULL DEFAULT NULL,
				move_state int(11) NULL DEFAULT NULL,
				PRIMARY KEY USING BTREE(uid)
			) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

			`CREATE TABLE v4_ai_user_temp (
				id int(11) NOT NULL,
				uid int(11) NULL DEFAULT NULL,
				x int(11) NULL DEFAULT NULL,
				y int(11) NULL DEFAULT NULL,
				ditu_id int(11) NULL DEFAULT NULL,
				fangjian_id int(11) NULL DEFAULT NULL,
				pingdao_id int(11) NULL DEFAULT NULL,
				state int(11) NULL DEFAULT NULL,
				login_time int(11) NULL DEFAULT NULL,
				PRIMARY KEY USING BTREE(id)
			) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,
		}
		for i, stmt := range v4Statements {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("v4_ai stmt %d: %w", i, err)
			}
		}
	}
	return nil
}

func migrateRobot(db *sql.DB) error {
	exists, err := tableExists(db, "d_starsky", "Robot")
	if err != nil {
		return fmt.Errorf("check Robot: %w", err)
	}
	if !exists {
		if _, err := db.Exec("USE d_starsky"); err != nil {
			return fmt.Errorf("use d_starsky: %w", err)
		}
		if _, err := db.Exec(`CREATE TABLE Robot (
			robot int(11) UNSIGNED NOT NULL DEFAULT 1,
			aiState int(11) UNSIGNED NOT NULL DEFAULT 1,
			speed int(11) UNSIGNED NOT NULL DEFAULT 200
		) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=COMPACT`); err != nil {
			return fmt.Errorf("create Robot: %w", err)
		}
		if _, err := db.Exec("INSERT INTO Robot (robot) VALUES (1)"); err != nil {
			return fmt.Errorf("insert Robot: %w", err)
		}
	}
	return nil
}

func migrateRobotStall(db *sql.DB) error {
	exists, err := tableExists(db, "d_starsky", "Robot_stall")
	if err != nil {
		return fmt.Errorf("check Robot_stall: %w", err)
	}
	if !exists {
		if _, err := db.Exec("USE d_starsky"); err != nil {
			return fmt.Errorf("use d_starsky: %w", err)
		}
		if _, err := db.Exec(`CREATE TABLE Robot_stall (
			id varchar(32) NOT NULL,
			Trade_name varchar(255) default NULL,
			Trade_item int(11) default NULL,
			price int(10) default NULL,
			function_type int(11) default NULL,
			UID int(11) default NULL,
			state int(11) default NULL,
			item_number int(11) default NULL,
			PRIMARY KEY(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8`); err != nil {
			return fmt.Errorf("create Robot_stall: %w", err)
		}
	}
	return nil
}

func migrateRobotStallConfig(db *sql.DB) error {
	exists, err := tableExists(db, "d_starsky", "Robot_stall_config")
	if err != nil {
		return fmt.Errorf("check Robot_stall_config: %w", err)
	}
	if !exists {
		if _, err := db.Exec("USE d_starsky"); err != nil {
			return fmt.Errorf("use d_starsky: %w", err)
		}
		if _, err := db.Exec(`CREATE TABLE Robot_stall_config (
			id varchar(32) NOT NULL,
			cfg_content varchar(2000) default NULL,
			cfg_type int(11) default NULL,
			function_type int(11) default NULL,
			UID int(11) default NULL,
			state int(11) default NULL,
			PRIMARY KEY(id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8`); err != nil {
			return fmt.Errorf("create Robot_stall_config: %w", err)
		}
	}
	return nil
}

func migrateTitles(db *sql.DB) error {
	exists, err := tableExists(db, "d_starsky", "titles")
	if err != nil {
		return fmt.Errorf("check titles: %w", err)
	}
	if !exists {
		if _, err := db.Exec("USE d_starsky"); err != nil {
			return fmt.Errorf("use d_starsky: %w", err)
		}
		if _, err := db.Exec(`CREATE TABLE titles (
			seal_ip int(11) UNSIGNED NOT NULL DEFAULT 1,
			seal_mac int(11) UNSIGNED NOT NULL DEFAULT 1,
			seal_uid int(11) UNSIGNED NOT NULL DEFAULT 1,
			seal_announcement int(11) UNSIGNED NOT NULL DEFAULT 1,
			seal_content varchar(500) CHARACTER SET utf8 COLLATE utf8_general_ci NULL DEFAULT NULL
		) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`); err != nil {
			return fmt.Errorf("create titles: %w", err)
		}
		if _, err := db.Exec("INSERT INTO titles (seal_ip, seal_mac, seal_uid, seal_announcement, seal_content) VALUES (1, 1, 1, 1, '255##5##玩家%s使用非法程序，已封号处理！请不要使用第三方程序')"); err != nil {
			return fmt.Errorf("insert titles: %w", err)
		}
	}
	return nil
}

func migrateGiftTaskTables(db *sql.DB) error {
	exists, err := tableExists(db, "d_starsky", "gift_task")
	if err != nil {
		return fmt.Errorf("check gift_task: %w", err)
	}
	if !exists {
		if _, err := db.Exec("USE d_starsky"); err != nil {
			return fmt.Errorf("use d_starsky: %w", err)
		}
		taskStatements := []string{
			`CREATE TABLE gift_task (
				task_id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				task_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				task_type int(10) NOT NULL,
				region_id int(10) NOT NULL,
				room_id int(10) NOT NULL,
				user_num int(10) NOT NULL,
				code_num int(10) NOT NULL,
				task_state int(10) NOT NULL,
				gift_odds int(10) NOT NULL,
				gift_type int(10) NOT NULL,
				task_time int(10) NOT NULL,
				task_last_time int(15) NOT NULL,
				name2 varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				time_check_type int(10) NOT NULL,
				time_check varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				PRIMARY KEY USING BTREE (task_id)
			) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=COMPACT`,

			`CREATE TABLE gift_history (
				id int(15) NOT NULL AUTO_INCREMENT,
				task_id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				create_time varchar(50) CHARACTER SET utf8 COLLATE utf8_general_ci NULL DEFAULT NULL,
				content varchar(3000) CHARACTER SET utf8 COLLATE utf8_general_ci NULL DEFAULT NULL,
				PRIMARY KEY USING BTREE (id)
			) ENGINE=InnoDB AUTO_INCREMENT=1 CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

			`CREATE TABLE gift_code (
				id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				task_id varchar(32) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				code_id int(15) NOT NULL,
				code_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				code_num int(10) NOT NULL,
				code_odds int(10) NOT NULL,
				code_state int(10) NOT NULL,
				PRIMARY KEY USING BTREE (id)
			) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=COMPACT`,

			`CREATE TABLE gift_user (
				cid int(11) NOT NULL,
				region int(10) NULL DEFAULT NULL,
				room int(10) NULL DEFAULT NULL,
				PRIMARY KEY USING BTREE (cid)
			) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,
		}
		for i, stmt := range taskStatements {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("gift_task stmt %d: %w", i, err)
			}
		}
	}
	return nil
}

func migrateMapTask(db *sql.DB) error {
	exists, err := tableExists(db, "d_starsky", "map_task")
	if err != nil {
		return fmt.Errorf("check map_task: %w", err)
	}
	if !exists {
		if _, err := db.Exec("USE d_starsky"); err != nil {
			return fmt.Errorf("use d_starsky: %w", err)
		}
		mapStatements := []string{
			`CREATE TABLE map_task (
				task_id int(11) NOT NULL AUTO_INCREMENT,
				task_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				map_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				reward_state int(10) NOT NULL,
				msg_state int(10) NOT NULL,
				msg_text varchar(500) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				task_state int(10) NOT NULL,
				msg_single_text varchar(500) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				is_steam_reward int(10) NOT NULL,
				PRIMARY KEY USING BTREE (task_id)
			) ENGINE=InnoDB AUTO_INCREMENT=2 CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

			`CREATE TABLE map_history (
				ID int(11) UNSIGNED NOT NULL AUTO_INCREMENT,
				uid int(11) NOT NULL,
				charactName varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				dungeonId int(11) NOT NULL,
				dungeonName varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				difficulty int(11) NOT NULL,
				mapCount int(11) NOT NULL,
				killMobCount int(11) NOT NULL,
				useCubepieceClearCount int(11) NOT NULL,
				hell int(11) NOT NULL,
				passingTime int(11) NOT NULL,
				clearInfo int(11) NOT NULL,
				success int(11) NOT NULL,
				leaveTime datetime NOT NULL,
				reward_state int(11) NOT NULL,
				is_captain int(11) NOT NULL,
				is_team int(11) NOT NULL,
				c_id int(11) NOT NULL,
				msg_text varchar(500) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				PRIMARY KEY USING BTREE (ID)
			) ENGINE=InnoDB AUTO_INCREMENT=18 CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,

			`CREATE TABLE map_code (
				id int(10) NOT NULL AUTO_INCREMENT,
				task_id int(10) NOT NULL,
				code_id int(15) NOT NULL,
				code_name varchar(255) CHARACTER SET utf8 COLLATE utf8_general_ci NOT NULL,
				code_num int(10) NOT NULL,
				code_odds int(10) NOT NULL,
				code_state int(10) NOT NULL,
				PRIMARY KEY USING BTREE (id)
			) ENGINE=InnoDB AUTO_INCREMENT=2 CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`,
		}
		for i, stmt := range mapStatements {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("map_task stmt %d: %w", i, err)
			}
		}
	}
	return nil
}

func migrateRobotAlter(db *sql.DB) error {
	exists, err := columnExists(db, "d_starsky", "Robot", "aiState")
	if err != nil {
		return fmt.Errorf("check Robot.aiState: %w", err)
	}
	if !exists {
		if _, err := db.Exec("ALTER TABLE d_starsky.Robot ADD COLUMN aiState int(11) UNSIGNED NOT NULL DEFAULT 1, ADD COLUMN speed int(11) UNSIGNED NOT NULL DEFAULT 200"); err != nil {
			return fmt.Errorf("alter Robot: %w", err)
		}
	}
	return nil
}

func migrateDummylistAlter(db *sql.DB) error {
	exists, err := columnExists(db, "d_starsky", "Dummylist", "ip")
	if err != nil {
		return fmt.Errorf("check Dummylist.ip: %w", err)
	}
	if !exists {
		alterations := []string{
			"ALTER TABLE d_starsky.Dummylist ADD COLUMN ip varchar(100) default NULL",
			"ALTER TABLE d_starsky.Dummylist ADD COLUMN function_type int(11) default NULL",
			"ALTER TABLE d_starsky.Dummylist ADD COLUMN discost int(11) default NULL",
		}
		for _, sqlStr := range alterations {
			if _, err := db.Exec(sqlStr); err != nil {
				return fmt.Errorf("%s: %w", sqlStr, err)
			}
		}
		if _, err := db.Exec("DELETE FROM d_starsky.Dummylist"); err != nil {
			return fmt.Errorf("clear Dummylist: %w", err)
		}
	}
	return nil
}

func recreateDataTmp(db *sql.DB) error {
	if _, err := db.Exec("DROP TABLE IF EXISTS d_starsky.dataTmp"); err != nil {
		return fmt.Errorf("drop dataTmp: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE d_starsky.dataTmp (
		dataObj mediumblob NULL
	) ENGINE=InnoDB CHARACTER SET utf8 COLLATE utf8_general_ci ROW_FORMAT=Compact`); err != nil {
		return fmt.Errorf("create dataTmp: %w", err)
	}
	return nil
}
