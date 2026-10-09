package com.xiaoqi.video.core.data;
import androidx.room.*;
import java.util.List;
@Dao public interface RecordDao {
 @Query("SELECT * FROM records WHERE owner=:owner AND bucket=:bucket ORDER BY updated DESC") List<StoredRecord> list(long owner,String bucket);
 @Query("SELECT * FROM records WHERE bucket=:bucket AND record_key=:key LIMIT 1") StoredRecord get(String bucket,String key);
 @Insert(onConflict=OnConflictStrategy.REPLACE) void put(StoredRecord record);
 @Query("DELETE FROM records WHERE bucket=:bucket AND record_key=:key") void remove(String bucket,String key);
 @Query("DELETE FROM records WHERE owner=:owner AND bucket=:bucket") void clear(long owner,String bucket);
}
