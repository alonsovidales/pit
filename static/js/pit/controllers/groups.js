var GroupsController = (function() {
	var groupsSecrets = {};
	var charts = {};

	var my = {
		getGroupsInfo: function(successCallback, errorCallback) {
			$.ajax({
				type: 'POST',
				url: cAPIBase + '/get_groups_by_user',
				data: {
					u: LoginController.getUser(),
					uk: LoginController.getKey(),
				},
				success: successCallback,
				error: errorCallback,
				dataType: 'json'
			});
		},

		init: function() {
			$('#new-group-form').submit(function (e) {
				e.preventDefault();
				addNewGroup();
			});

			this.getGroupsInfo(plotGroupInfo);
		}
	};

	var sanitize = function(k) {
		return k.replace(/\./g, "-").replace(/:/g, "-");
	};

	var addNewGroup = function() {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/add_group',
			data: {
				u: LoginController.getUser(),
				uk: LoginController.getKey(),
				guid: $('#new-group-name').val(),
				gt: $('#new-group-type').val(),
				shards: $('#new-group-shards').val(),
				maxscore: $('#new-group-max-score').val()
			},
			success: function() {
				location.reload();
			},
			error: function(msg) {
				alert(msg.responseText);
			},
			dataType: 'json'
		});
	};

	var getShardsInfo = function(groupId, callback) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/info',
			data: {
				uid: LoginController.getUser(),
				key: groupsSecrets[groupId],
				group: groupId
			},
			success: callback,
			dataType: 'json'
		});
	}

	var roundTwoDec = function(n) {
		return Math.round(n * 100) / 100
	};

	var updateGroupKey = function(groupId) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/generate_group_key',
			data: {
				u: LoginController.getUser(),
				uk: LoginController.getKey(),
				g: groupId,
				k: groupsSecrets[groupId]
			},
			success: function(newKey) {
				$("#group-" + sanitize(groupId) + "-key").text(newKey);
				groupsSecrets[groupId] = newKey;
			}
		});
	};

	var updateShardsInfo = function (groupId) {
		setInterval(function () {
			getShardsInfo(groupId, function (data) {
				var x = (new Date()).getTime()
				$.each(data, function(k, v) {
					var shardIdSanit = sanitize(k);
					var sanitGroupID = sanitize(groupId);
					var secVal = v.queries_by_sec[v.queries_by_sec.length-1];
					var perc = roundTwoDec(secVal / ~~$("#group-" + sanitGroupID + "-req-sec").text() * 100);
					if (perc > 100) {
						perc = 100;
					}
					$("#shard-req-sec-" + sanitGroupID + "-" + shardIdSanit).text(secVal);
					var barDiv = $("#shard-req-sec-prog-bar-" + sanitGroupID + "-" + shardIdSanit);
					barDiv.css("width", perc + "%");
					barDiv.text(perc + "%");

					$("#shard-elems-stored-" + sanitGroupID + "-" + shardIdSanit).text(v.stored_elements);
					var percElems = roundTwoDec(v.stored_elements / ~~$("#group-" + sanitGroupID + "-max-elements").text() * 100);
					var barDivElems = $("#shard-elems-stored-prog-bar-" + sanitGroupID + "-" + shardIdSanit);
					barDivElems.css("width", percElems + "%");
					barDivElems.text(percElems + "%");

					$("#shard-status-" + sanitGroupID + "-" + shardIdSanit).text(v.rec_tree_status);

					if (perc > 80) {
						barDiv.addClass("progress-bar-danger");
						barDiv.removeClass("progress-bar-warning");
					} else if (perc > 65) {
						barDiv.addClass("progress-bar-warning");
						barDiv.removeClass("progress-bar-danger");
					} else {
						barDiv.removeClass("progress-bar-danger");
						barDiv.removeClass("progress-bar-warning");
					}

					charts[sanitGroupID + "-sec-" + shardIdSanit].addPoint([x, secVal], true, true);
					if (Math.round(x/1000) % 60 === 0) {
						var minVal = v.queries_by_min[v.queries_by_min.length-1];
						charts[sanitGroupID + "-min-" + shardIdSanit].addPoint([x, minVal], true, true);
					}
				});
			});
		}, 1000);
	};

	var addAnimatedChar = function(title, targetDiv, id, initialInfo, timeMult) {
		if (initialInfo.length > 1000) {
			initialInfo = initialInfo.slice(initialInfo.length-1000);
		}
		targetDiv.highcharts({
			chart: {
				type: 'spline',
				animation: Highcharts.svg,
				marginRight: 10,
				events: {
					load: function () {
						charts[id] = this.series[0];
					}
				}
			},
			title: {
				text: null,
			},
			xAxis: {
				type: 'datetime',
				tickPixelInterval: 150
			},
			yAxis: {
				title: {
					text: null,
				},
				plotLines: [{
					value: 0,
					width: 1,
					color: '#808080'
				}]
			},
			tooltip: {
				formatter: function () {
					return '<b>' + this.series.name + '</b><br/>' +
						Highcharts.dateFormat('%Y-%m-%d %H:%M:%S', this.x) + '<br/>' +
						Highcharts.numberFormat(this.y, 2);
				}
			},
			legend: {
				enabled: false
			},
			exporting: {
				enabled: false
			},
			series: [{
				name: title,
				data: (function () {
					var data = [],
					time = (new Date()).getTime(),
					i;

					for (i = 0; i < initialInfo.length; i++) {
						data.push({
							x: time - (initialInfo.length - i) * (timeMult * 1000),
							y: initialInfo[i],
						});
					}
					return data;
				}())
			}]
		});
	}

	var removeGroup = function(groupId) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/del_group',
			data: {
				u: LoginController.getUser(),
				uk: LoginController.getKey(),
				k: groupsSecrets[groupId],
				g: groupId,
			},
			success: function(newKey) {
				alert("Group removed");
				location.reload();
			}
		});
	};

	var removeShardsContent = function(groupId) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/remove_group_shards_content',
			data: {
				u: LoginController.getUser(),
				uk: LoginController.getKey(),
				k: groupsSecrets[groupId],
				g: groupId,
			},
			success: function(newKey) {
				alert("Content removed, this action can take some time to have effect, please be pattient");
			}
		});
	};

	var updateShardsGroup = function(groupId, numShards) {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/set_shards_group',
			data: {
				u: LoginController.getUser(),
				uk: LoginController.getKey(),
				g: groupId,
				k: groupsSecrets[groupId],
				s: numShards
			},
			success: function(newKey) {
				alert("Updated");
			}
		});
	};

	var plotGroupInfo = function(data) {
		Highcharts.setOptions({
			global: {
				useUTC: false
			}
		});
		var groupsTemplate = Handlebars.compile($("#groups-template").html());
		var shardsTemplate = Handlebars.compile($("#shards-template").html());
		var containerDiv = $("#shads-container");

		containerDiv.html('');

		if (data === null) {
			data = [];
			$('#shads-container').text('No groups defined');
		}
		$.each(data, function (k, v) {
			v.group_id = sanitize(v.group_id);
			var html = groupsTemplate(v);

			groupsSecrets[k] = v.secret;
			containerDiv.append(html);

			$("#shards-update-buttn-" + sanitize(k)).click(function() {
				updateShardsGroup(k, ~~$("#shards-update-txt-" + sanitize(k)).val());
			});

			$("#group-button-" + sanitize(k) + "-del-group").click(function() {
				if (confirm('This action will remove all the content from the shards, stored backups and configuration, this action can\'t be undone. Are you completly sure that you want to perform this action?')) {
					removeGroup(k);
				}
			});

			$("#group-button-" + sanitize(k) + "-remove-all").click(function() {
				if (confirm('This action will remove all the content from the shards and stored backups, this action can\'t be undone. Are you completly sure that you want to perform this action?')) {
					removeShardsContent(k);
				}
			});

			$("#group-button-" + sanitize(k) + "-key").click(function() {
				if (confirm('Are you sure that you want to regenerate the key for this group?, remember change it on all the clients')) {
					updateGroupKey(k);
				}
			});

			getShardsInfo(k, function(shardsData) {
				var shardsGroupContainer = $("#group-shards-" + sanitize(k));
				$.each(shardsData, function(host, shardInfo) {
					var statusLevel;

					switch(shardInfo.rec_tree_status) {
						case "STARTING":
							statusLevel = "primary";
							break;
						case "LOADING":
							statusLevel = "info";
							break;
						case "ACTIVE":
							statusLevel = "success";
							break;
						case "NO_RECORDS":
							statusLevel = "warning";
							break;
					}

					var reqsSec = shardInfo.queries_by_sec[shardInfo.queries_by_sec.length-1];
					var hostSanit = sanitize(host);
					var templateInfo = {
						group_id: sanitize(k),
						shard_id_full: host,
						shard_id: hostSanit,
						elems_stored: shardInfo.stored_elements,
						reqs_sec: reqsSec,
						status_level: statusLevel,
						perc_stored: roundTwoDec((shardInfo.stored_elements / v.max_elems) * 100),
						perc_reqs: roundTwoDec((reqsSec / v.max_req_sec) * 100),
						shard_status: shardInfo.rec_tree_status
					};
					shardsGroupContainer.append(shardsTemplate(templateInfo));

					// Add the animated chart at the bottom
					addAnimatedChar("Req/sec", $("#req-sec-stats-" + sanitize(k) + "-" + hostSanit), sanitize(k) + "-sec-" + hostSanit, shardInfo.queries_by_sec, 1);
					addAnimatedChar("Req/min", $("#req-min-stats-" + sanitize(k) + "-" + hostSanit), sanitize(k) + "-min-" + hostSanit, shardInfo.queries_by_min, 60);
				});
			});

			updateShardsInfo(k);
		});
	};

	return my;
})();
