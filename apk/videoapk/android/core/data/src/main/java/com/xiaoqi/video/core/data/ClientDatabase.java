package com.xiaoqi.video.core.data;
import androidx.room.*;
@Database(entities={StoredRecord.class},version=1,exportSchema=false)
public abstract class ClientDatabase extends RoomDatabase { public abstract RecordDao records(); }

