package com.xiaoqi.video.core.data;
import androidx.annotation.NonNull;
import androidx.room.Entity;
import androidx.room.Index;
import androidx.room.ColumnInfo;
@Entity(tableName="records", primaryKeys={"bucket","record_key"}, indices={@Index(value={"owner","bucket"})})
public class StoredRecord {
 @NonNull public String bucket = "";
 @NonNull @ColumnInfo(name="record_key") public String key = "";
 public long owner;
 @NonNull public String json = "";
 public long updated;
 public StoredRecord() {}
 public StoredRecord(@NonNull String bucket,@NonNull String key,long owner,@NonNull String json,long updated) { this.bucket=bucket;this.key=key;this.owner=owner;this.json=json;this.updated=updated; }
}
