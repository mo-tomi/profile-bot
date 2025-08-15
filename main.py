def get_member_display_name(member):
    """
    メンバーの表示名を安全に取得する関数
    優先順位：
    1. USER_NAME_MAPPINGでの特別設定
    2. Discordユーザー名（@username）を最優先
    3. サーバーニックネーム
    4. グローバル表示名（Discordの表示名）
    5. フォールバック
    """
    # 特定のユーザーIDに対するカスタム名前をチェック
    if member.id in USER_NAME_MAPPING:
        return USER_NAME_MAPPING[member.id]
    
    # Discordユーザー名（@username）を最優先
    if member.name:
        return member.name
    
    # サーバーニックネームがあればそれを使用
    if member.nick:
        return member.nick
    
    # グローバル表示名（Discordプロフィールの表示名）
    if hasattr(member, 'global_name') and member.global_name:
        return member.global_name
    
    # それでも取得できない場合
    return f"ユーザー{member.id}"

@bot.event
async def on_voice_state_update(member, before, after):
    # 特定のbotと管理人の自己紹介を除外
    excluded_bot_ids = [533698325203910668, 916300992612540467, 1300226846599675974]
    
    if (before.channel != after.channel and 
        after.channel and 
        after.channel.id in TARGET_VOICE_CHANNELS):
        
        # 除外対象のbotかチェック
        if member.id in excluded_bot_ids:
            member_name = get_member_display_name(member)
            logging.info(f"🤖 除外対象bot {member_name} (ID: {member.id}) がボイスチャンネル '{after.channel.name}' に参加しましたが、自己紹介通知をスキップします")
            return
        
        # メンバーの表示名を取得（ユーザー名を優先）
        member_name = get_member_display_name(member)
        
        # デバッグ用：どの名前が使われているか確認
        logging.info(f"🔍 名前情報 - Nick: {member.nick}, Global: {getattr(member, 'global_name', 'N/A')}, Name: {member.name}")
        logging.info(f"🔊 {member_name} (ID: {member.id}) がボイスチャンネル '{after.channel.name}' に参加しました")
        
        notify_channel = bot.get_channel(NOTIFICATION_CHANNEL_ID)
        if not notify_channel:
            logging.error(f"❌ 通知チャンネル(ID: {NOTIFICATION_CHANNEL_ID})が見つかりません")
            return
        
        try:
            logging.info(f"🔍 {member_name} の自己紹介を検索中...")
            intro_ids = await db.get_intro_ids(member.id)
            
            if intro_ids:
                logging.info(f"✅ 自己紹介発見: Channel {intro_ids['channel_id']}, Message {intro_ids['message_id']}")
                
                try:
                    intro_channel = bot.get_channel(intro_ids['channel_id'])
                    if not intro_channel:
                        logging.error(f"❌ 自己紹介チャンネル(ID: {intro_ids['channel_id']})が取得できません")
                        raise Exception("チャンネル取得失敗")
                    
                    intro_message = await intro_channel.fetch_message(intro_ids['message_id'])
                    logging.info(f"✅ 自己紹介メッセージ取得成功 (長さ: {len(intro_message.content)}文字)")
                    
                    # シンプルにユーザー名をそのまま使用
                    display_name = member_name
                    
                    embed = discord.Embed(
                        description=intro_message.content, 
                        color=discord.Color.blue()
                    )
                    embed.set_author(
                        name=f"{display_name}さんの自己紹介",
                        icon_url=member.display_avatar.url
                    )
                    
                    view = ui.View()
                    button = ui.Button(
                        label="元の自己紹介へ移動", 
                        style=discord.ButtonStyle.link, 
                        url=intro_message.jump_url
                    )
                    view.add_item(button)
                    
                    # ユーザー名をそのまま表示
                    await notify_channel.send(
                        f"**{display_name}** さんが `{after.channel.name}` に入室しました！", 
                        embed=embed, 
                        view=view
                    )
                    logging.info("✅ 自己紹介付き通知を送信しました")
                    
                except discord.NotFound:
                    logging.warning(f"⚠️ {member_name} の自己紹介メッセージが見つかりません（削除済み?）")
                    msg = f"**{member_name}** さんが `{after.channel.name}` に入室しました！\n⚠️ この方の自己紹介メッセージが削除されているようです。"
                    await notify_channel.send(msg)
                    logging.info("✅ 自己紹介なし通知（削除済み）を送信しました")
                    
                except Exception as fetch_error:
                    logging.error(f"❌ 自己紹介メッセージ取得エラー: {fetch_error}")
                    msg = f"**{member_name}** さんが `{after.channel.name}` に入室しました！\n⚠️ 自己紹介の取得中にエラーが発生しました。"
                    await notify_channel.send(msg)
                    logging.info("✅ エラー時代替通知を送信しました")
            else:
                logging.info(f"❌ {member_name} の自己紹介がDBに見つかりません")
                msg = f"**{member_name}** さんが `{after.channel.name}` に入室しました！\n⚠️ この方の自己紹介はまだ投稿されていないか、見つかりませんでした。"
                await notify_channel.send(msg)
                logging.info("✅ 自己紹介なし通知を送信しました")
                
        except Exception as e:
            logging.error(f"❌ 通知処理中にエラー: {e}", exc_info=True)
            
            try:
                msg = f"**{member_name}** さんが `{after.channel.name}` に入室しました！"
                await notify_channel.send(msg)
                logging.info("✅ 最低限の入室通知を送信しました")
            except Exception as fallback_error:
                logging.error(f"❌ 代替通知送信も失敗: {fallback_error}")
